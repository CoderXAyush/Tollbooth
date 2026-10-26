package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CoderXAyush/Tollbooth/services/woo-mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	priceRegex = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)
	urlRegex   = regexp.MustCompile(`https?://[^\s"'>]+`)
	ipRegex    = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(?::\d+)?\b`)
	hostRegex  = regexp.MustCompile(`\b[a-zA-Z0-9.-]+\.(?:tollbooth|svc|cluster\.local|nip\.io)(?::\d+)?\b`)

	validOrderStatuses = map[string]bool{
		"pending":    true,
		"processing": true,
		"on-hold":    true,
		"completed":  true,
		"cancelled":  true,
		"refunded":   true,
		"failed":     true,
		"trash":      true,
	}

	validUpdateOrderStatuses = map[string]bool{
		"pending":    true,
		"processing": true,
		"on-hold":    true,
		"completed":  true,
		"cancelled":  true,
		"failed":     true,
	}

	validProductStatuses = map[string]bool{
		"publish": true,
		"draft":   true,
		"pending": true,
		"private": true,
	}

	boolFalse = false
	boolTrue  = true
)

// ── Tool Input / Output Definitions ──────────────────────────

// ListProductsInput represents inputs for listing products.
type ListProductsInput struct {
	Page    int    `json:"page,omitempty" jsonschema:"Page number to retrieve (starts at 1)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"Number of items per page (1-50, default 20)"`
	Search  string `json:"search,omitempty" jsonschema:"Search keyword to filter products by name or description"`
}

// ProductSummary represents trimmed product information.
type ProductSummary struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Price        string `json:"price"`
	RegularPrice string `json:"regular_price"`
	Status       string `json:"status"`
	Type         string `json:"type"`
	Description  string `json:"description,omitempty"`
	Permalink    string `json:"permalink,omitempty"`
}

// GetProductInput represents inputs for retrieving a product.
type GetProductInput struct {
	ID int `json:"id" jsonschema:"Unique product ID"`
}

// ProductDetail represents detailed product information.
type ProductDetail struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Price        string   `json:"price"`
	RegularPrice string   `json:"regular_price"`
	SalePrice    string   `json:"sale_price,omitempty"`
	Status       string   `json:"status"`
	Type         string   `json:"type"`
	Description  string   `json:"description,omitempty"`
	StockStatus  string   `json:"stock_status"`
	Categories   []string `json:"categories,omitempty"`
	Permalink    string   `json:"permalink,omitempty"`
}

// CreateProductInput represents inputs for creating a simple product.
type CreateProductInput struct {
	Name         string `json:"name" jsonschema:"Product name"`
	RegularPrice string `json:"regular_price" jsonschema:"Non-negative decimal price string (e.g. 12.50 or 10)"`
	Description  string `json:"description,omitempty" jsonschema:"Optional product description"`
	Status       string `json:"status,omitempty" jsonschema:"Product status: publish, draft, pending, or private (defaults to publish)"`
	Type         string `json:"type,omitempty" jsonschema:"Product type (defaults to simple)"`
}

// ListOrdersInput represents inputs for listing orders.
type ListOrdersInput struct {
	Status  string `json:"status,omitempty" jsonschema:"Order status filter: any, pending, processing, on-hold, completed, cancelled, refunded, failed, trash"`
	After   string `json:"after,omitempty" jsonschema:"Limit results to orders created after this UTC date/time in ISO 8601 format (e.g. 2026-10-25T00:00:00Z)"`
	Before  string `json:"before,omitempty" jsonschema:"Limit results to orders created before this UTC date/time in ISO 8601 format (e.g. 2026-10-25T23:59:59Z)"`
	Page    int    `json:"page,omitempty" jsonschema:"Page number to retrieve (starts at 1)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"Number of items per page (1-50, default 20)"`
}

// OrderSummary represents trimmed order information without PII.
type OrderSummary struct {
	ID                 int               `json:"id"`
	Status             string            `json:"status"`
	Total              string            `json:"total"`
	Currency           string            `json:"currency"`
	DateCreatedGMT     string            `json:"date_created_gmt"`
	PaymentMethodTitle string            `json:"payment_method_title"`
	LineItems          []LineItemSummary `json:"line_items"`
}

// LineItemSummary represents a trimmed line item for order listings.
type LineItemSummary struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Total    string `json:"total"`
}

// GetOrderInput represents inputs for retrieving order details.
type GetOrderInput struct {
	ID int `json:"id" jsonschema:"Unique order ID"`
}

// CustomerContact represents limited customer contact details (name and email only; no street or phone).
type CustomerContact struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

// LineItemDetail represents detailed line item information in an order.
type LineItemDetail struct {
	ProductID int    `json:"product_id"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	Subtotal  string `json:"subtotal"`
	Total     string `json:"total"`
}

// OrderDetail represents detailed order information.
type OrderDetail struct {
	ID                 int              `json:"id"`
	Status             string           `json:"status"`
	Total              string           `json:"total"`
	Currency           string           `json:"currency"`
	DateCreatedGMT     string           `json:"date_created_gmt"`
	PaymentMethodTitle string           `json:"payment_method_title"`
	Customer           CustomerContact  `json:"customer"`
	LineItems          []LineItemDetail `json:"line_items"`
}

// UpdateOrderStatusInput represents inputs for updating order status.
type UpdateOrderStatusInput struct {
	ID     int    `json:"id" jsonschema:"Unique order ID"`
	Status string `json:"status" jsonschema:"New order status: pending, processing, on-hold, completed, cancelled, failed"`
}

// OrderStatusUpdateResult represents the result of updating an order's status.
type OrderStatusUpdateResult struct {
	ID           int    `json:"id"`
	Status       string `json:"status"`
	DateModified string `json:"date_modified"`
}

// ── Helpers ──────────────────────────────────────────────────

// toolErrorResult formats business or validation failures as an MCP tool result with isError=true.
func toolErrorResult[T any](msg string) (*mcp.CallToolResult, T, error) {
	var zero T
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}, zero, nil
}

// toolSuccessResult formats successful output as JSON in Content and sets StructuredContent.
func toolSuccessResult[T any](val T) (*mcp.CallToolResult, T, error) {
	jsonBytes, err := json.Marshal(val)
	if err != nil {
		return toolErrorResult[T](fmt.Sprintf("internal error serializing response: %v", err))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(jsonBytes)},
		},
		StructuredContent: val,
	}, val, nil
}

// sanitizeText removes hostnames, IPs, ports, and URLs from text to prevent leakage.
func sanitizeText(text string) string {
	text = urlRegex.ReplaceAllString(text, "[redacted-url]")
	text = ipRegex.ReplaceAllString(text, "[redacted-ip]")
	text = hostRegex.ReplaceAllString(text, "[redacted-host]")
	return text
}

// formatRFC3339Z parses timestamp and formats as RFC3339 in UTC with Z suffix.
func formatRFC3339Z(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	if !strings.HasSuffix(s, "Z") {
		return s + "Z"
	}
	return s
}

// mapClientError maps client/network errors to sanitised tool error messages.
func mapClientError[T any](err error) (*mcp.CallToolResult, T, error) {
	slog.Error("woocommerce upstream client error", "err", err)
	if errors.Is(err, context.DeadlineExceeded) {
		return toolErrorResult[T]("WooCommerce request timed out")
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		cleanMsg := sanitizeText(apiErr.Message)
		return toolErrorResult[T](fmt.Sprintf("WooCommerce API error (%d): %s", apiErr.StatusCode, cleanMsg))
	}
	return toolErrorResult[T]("WooCommerce upstream request failed")
}

// parseFlexibleISO8601 parses standard ISO 8601 strings (RFC3339, dates without offset, or date-only).
func parseFlexibleISO8601(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid ISO 8601 date: %q (expected format like '2026-10-25T00:00:00Z')", s)
}

// ── Tool Handlers ─────────────────────────────────────────────

func handleListProducts(woo client.Client) mcp.ToolHandlerFor[ListProductsInput, []ProductSummary] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ListProductsInput) (*mcp.CallToolResult, []ProductSummary, error) {
		page := input.Page
		if page < 1 {
			page = 1
		}
		perPage := input.PerPage
		if perPage <= 0 {
			perPage = 20
		} else if perPage > 50 {
			return toolErrorResult[[]ProductSummary]("per_page must not exceed 50")
		}

		products, err := woo.ListProducts(ctx, client.ListProductsFilter{
			Page:    page,
			PerPage: perPage,
			Search:  strings.TrimSpace(input.Search),
		})
		if err != nil {
			return mapClientError[[]ProductSummary](err)
		}

		summaries := make([]ProductSummary, 0, len(products))
		for _, p := range products {
			desc := strings.TrimSpace(p.ShortDesc)
			if desc == "" {
				desc = strings.TrimSpace(p.Description)
			}
			// Strip HTML tags for clean model consumption
			desc = stripHTML(desc)
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}

			summaries = append(summaries, ProductSummary{
				ID:           p.ID,
				Name:         p.Name,
				Slug:         p.Slug,
				Price:        p.Price,
				RegularPrice: p.RegularPrice,
				Status:       p.Status,
				Type:         p.Type,
				Description:  desc,
				Permalink:    p.Permalink,
			})
		}
		return toolSuccessResult(summaries)
	}
}

func handleGetProduct(woo client.Client) mcp.ToolHandlerFor[GetProductInput, *ProductDetail] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input GetProductInput) (*mcp.CallToolResult, *ProductDetail, error) {
		if input.ID <= 0 {
			return toolErrorResult[*ProductDetail]("id must be a positive integer")
		}

		p, err := woo.GetProduct(ctx, input.ID)
		if err != nil {
			return mapClientError[*ProductDetail](err)
		}

		categories := make([]string, 0, len(p.Categories))
		for _, cat := range p.Categories {
			categories = append(categories, cat.Name)
		}

		desc := stripHTML(strings.TrimSpace(p.Description))
		detail := &ProductDetail{
			ID:           p.ID,
			Name:         p.Name,
			Slug:         p.Slug,
			Price:        p.Price,
			RegularPrice: p.RegularPrice,
			SalePrice:    p.SalePrice,
			Status:       p.Status,
			Type:         p.Type,
			Description:  desc,
			StockStatus:  p.StockStatus,
			Categories:   categories,
			Permalink:    p.Permalink,
		}
		return toolSuccessResult(detail)
	}
}

func handleCreateProduct(woo client.Client) mcp.ToolHandlerFor[CreateProductInput, *ProductSummary] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input CreateProductInput) (*mcp.CallToolResult, *ProductSummary, error) {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			return toolErrorResult[*ProductSummary]("name is required and cannot be empty")
		}

		price := strings.TrimSpace(input.RegularPrice)
		if price == "" {
			return toolErrorResult[*ProductSummary]("regular_price is required")
		}
		if !priceRegex.MatchString(price) {
			return toolErrorResult[*ProductSummary]("regular_price must be a valid non-negative decimal string (e.g. '12.50' or '10')")
		}
		if parsed, err := strconv.ParseFloat(price, 64); err != nil || parsed < 0 {
			return toolErrorResult[*ProductSummary]("regular_price must be non-negative")
		}

		status := strings.ToLower(strings.TrimSpace(input.Status))
		if status == "" {
			status = "publish"
		} else if !validProductStatuses[status] {
			return toolErrorResult[*ProductSummary]("status must be one of: publish, draft, pending, private")
		}

		pType := strings.ToLower(strings.TrimSpace(input.Type))
		if pType == "" {
			pType = "simple"
		} else if pType != "simple" {
			return toolErrorResult[*ProductSummary]("type must be 'simple'")
		}

		created, err := woo.CreateProduct(ctx, client.CreateProductRequest{
			Name:         name,
			RegularPrice: price,
			Description:  strings.TrimSpace(input.Description),
			Status:       status,
			Type:         pType,
		})
		if err != nil {
			return mapClientError[*ProductSummary](err)
		}

		res := &ProductSummary{
			ID:           created.ID,
			Name:         created.Name,
			Slug:         created.Slug,
			Price:        created.Price,
			RegularPrice: created.RegularPrice,
			Status:       created.Status,
			Type:         created.Type,
			Description:  stripHTML(created.Description),
			Permalink:    created.Permalink,
		}
		return toolSuccessResult(res)
	}
}

func handleListOrders(woo client.Client) mcp.ToolHandlerFor[ListOrdersInput, []OrderSummary] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ListOrdersInput) (*mcp.CallToolResult, []OrderSummary, error) {
		page := input.Page
		if page < 1 {
			page = 1
		}
		perPage := input.PerPage
		if perPage <= 0 {
			perPage = 20
		} else if perPage > 50 {
			return toolErrorResult[[]OrderSummary]("per_page must not exceed 50")
		}

		status := strings.ToLower(strings.TrimSpace(input.Status))
		if status != "" && status != "any" && !validOrderStatuses[status] {
			return toolErrorResult[[]OrderSummary]("invalid status; valid options: any, pending, processing, on-hold, completed, cancelled, refunded, failed, trash")
		}

		var afterTime *time.Time
		if input.After != "" {
			t, err := parseFlexibleISO8601(input.After)
			if err != nil {
				return toolErrorResult[[]OrderSummary](fmt.Sprintf("invalid after date: %v", err))
			}
			afterTime = &t
		}

		var beforeTime *time.Time
		if input.Before != "" {
			t, err := parseFlexibleISO8601(input.Before)
			if err != nil {
				return toolErrorResult[[]OrderSummary](fmt.Sprintf("invalid before date: %v", err))
			}
			beforeTime = &t
		}

		orders, err := woo.ListOrders(ctx, client.ListOrdersFilter{
			Page:    page,
			PerPage: perPage,
			Status:  status,
			After:   afterTime,
			Before:  beforeTime,
		})
		if err != nil {
			return mapClientError[[]OrderSummary](err)
		}

		summaries := make([]OrderSummary, 0, len(orders))
		for _, o := range orders {
			items := make([]LineItemSummary, 0, len(o.LineItems))
			for _, item := range o.LineItems {
				items = append(items, LineItemSummary{
					Name:     item.Name,
					Quantity: item.Quantity,
					Total:    item.Total,
				})
			}

			// PII policy: omit phone, street addresses, and customer names in order listing
			summaries = append(summaries, OrderSummary{
				ID:                 o.ID,
				Status:             o.Status,
				Total:              o.Total,
				Currency:           o.Currency,
				DateCreatedGMT:     formatRFC3339Z(o.DateCreatedGMT),
				PaymentMethodTitle: o.PaymentMethodTitle,
				LineItems:          items,
			})
		}
		return toolSuccessResult(summaries)
	}
}

func handleGetOrder(woo client.Client) mcp.ToolHandlerFor[GetOrderInput, *OrderDetail] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input GetOrderInput) (*mcp.CallToolResult, *OrderDetail, error) {
		if input.ID <= 0 {
			return toolErrorResult[*OrderDetail]("id must be a positive integer")
		}

		o, err := woo.GetOrder(ctx, input.ID)
		if err != nil {
			return mapClientError[*OrderDetail](err)
		}

		items := make([]LineItemDetail, 0, len(o.LineItems))
		for _, item := range o.LineItems {
			items = append(items, LineItemDetail{
				ProductID: item.ProductID,
				Name:      item.Name,
				Quantity:  item.Quantity,
				Subtotal:  item.Subtotal,
				Total:     item.Total,
			})
		}

		// PII policy: include customer first_name, last_name, email only (no street address or phone)
		detail := &OrderDetail{
			ID:                 o.ID,
			Status:             o.Status,
			Total:              o.Total,
			Currency:           o.Currency,
			DateCreatedGMT:     formatRFC3339Z(o.DateCreatedGMT),
			PaymentMethodTitle: o.PaymentMethodTitle,
			Customer: CustomerContact{
				FirstName: o.Billing.FirstName,
				LastName:  o.Billing.LastName,
				Email:     o.Billing.Email,
			},
			LineItems: items,
		}
		return toolSuccessResult(detail)
	}
}

func handleUpdateOrderStatus(woo client.Client) mcp.ToolHandlerFor[UpdateOrderStatusInput, *OrderStatusUpdateResult] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateOrderStatusInput) (*mcp.CallToolResult, *OrderStatusUpdateResult, error) {
		if input.ID <= 0 {
			return toolErrorResult[*OrderStatusUpdateResult]("id must be a positive integer")
		}

		status := strings.ToLower(strings.TrimSpace(input.Status))
		if !validUpdateOrderStatuses[status] {
			return toolErrorResult[*OrderStatusUpdateResult]("invalid status; valid options: pending, processing, on-hold, completed, cancelled, failed")
		}

		o, err := woo.UpdateOrderStatus(ctx, input.ID, status)
		if err != nil {
			return mapClientError[*OrderStatusUpdateResult](err)
		}

		dateMod := o.DateModifiedGMT
		if dateMod == "" {
			dateMod = o.DateModified
		}
		res := &OrderStatusUpdateResult{
			ID:           o.ID,
			Status:       o.Status,
			DateModified: formatRFC3339Z(dateMod),
		}
		return toolSuccessResult(res)
	}
}

// stripHTML removes basic HTML tags from descriptions.
func stripHTML(s string) string {
	tagRegex := regexp.MustCompile(`<[^>]*>`)
	cleaned := tagRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(cleaned)
}

// RegisterTools registers all WooCommerce tools with the given MCP server.
func RegisterTools(server *mcp.Server, woo client.Client) {
	// 1. list_products
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_products",
		Title:       "List Products",
		Description: "List products from the WooCommerce catalog with optional filtering by search keyword and pagination.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: &boolFalse,
			IdempotentHint:  true,
		},
	}, handleListProducts(woo))

	// 2. get_product
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_product",
		Title:       "Get Product",
		Description: "Retrieve detailed information for a single WooCommerce product by its unique ID.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: &boolFalse,
			IdempotentHint:  true,
		},
	}, handleGetProduct(woo))

	// 3. create_product
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_product",
		Title:       "Create Product",
		Description: "Create a new simple product in the WooCommerce catalog.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: &boolFalse,
			IdempotentHint:  false,
		},
	}, handleCreateProduct(woo))

	// 4. list_orders
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_orders",
		Title:       "List Orders",
		Description: "List WooCommerce store orders with optional status, date range (ISO 8601 UTC), and pagination filters. All date filters ('after' and 'before') are evaluated in UTC against order creation times in UTC (date_created_gmt).",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: &boolFalse,
			IdempotentHint:  true,
		},
	}, handleListOrders(woo))

	// 5. get_order
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_order",
		Title:       "Get Order",
		Description: "Retrieve details of a specific WooCommerce order by ID, returning UTC timestamp (date_created_gmt), line items, and customer contact name/email.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: &boolFalse,
			IdempotentHint:  true,
		},
	}, handleGetOrder(woo))

	// 6. update_order_status
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_order_status",
		Title:       "Update Order Status",
		Description: "Update the status of an existing WooCommerce order (allowed: pending, processing, on-hold, completed, cancelled, failed).",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: &boolFalse,
			IdempotentHint:  true,
		},
	}, handleUpdateOrderStatus(woo))
}
