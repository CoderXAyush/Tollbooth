package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/CoderXAyush/Tollbooth/services/woo-mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockWooClient implements client.Client for testing.
type mockWooClient struct {
	listProductsFunc     func(ctx context.Context, filter client.ListProductsFilter) ([]client.WooProduct, error)
	getProductFunc       func(ctx context.Context, id int) (*client.WooProduct, error)
	createProductFunc    func(ctx context.Context, req client.CreateProductRequest) (*client.WooProduct, error)
	listOrdersFunc       func(ctx context.Context, filter client.ListOrdersFilter) ([]client.WooOrder, error)
	getOrderFunc         func(ctx context.Context, id int) (*client.WooOrder, error)
	updateOrderStatusFunc func(ctx context.Context, id int, status string) (*client.WooOrder, error)
}

func (m *mockWooClient) ListProducts(ctx context.Context, filter client.ListProductsFilter) ([]client.WooProduct, error) {
	if m.listProductsFunc != nil {
		return m.listProductsFunc(ctx, filter)
	}
	return nil, nil
}

func (m *mockWooClient) GetProduct(ctx context.Context, id int) (*client.WooProduct, error) {
	if m.getProductFunc != nil {
		return m.getProductFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockWooClient) CreateProduct(ctx context.Context, req client.CreateProductRequest) (*client.WooProduct, error) {
	if m.createProductFunc != nil {
		return m.createProductFunc(ctx, req)
	}
	return nil, nil
}

func (m *mockWooClient) ListOrders(ctx context.Context, filter client.ListOrdersFilter) ([]client.WooOrder, error) {
	if m.listOrdersFunc != nil {
		return m.listOrdersFunc(ctx, filter)
	}
	return nil, nil
}

func (m *mockWooClient) GetOrder(ctx context.Context, id int) (*client.WooOrder, error) {
	if m.getOrderFunc != nil {
		return m.getOrderFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockWooClient) UpdateOrderStatus(ctx context.Context, id int, status string) (*client.WooOrder, error) {
	if m.updateOrderStatusFunc != nil {
		return m.updateOrderStatusFunc(ctx, id, status)
	}
	return nil, nil
}

func TestListProducts(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock := &mockWooClient{
			listProductsFunc: func(_ context.Context, f client.ListProductsFilter) ([]client.WooProduct, error) {
				if f.Page != 1 || f.PerPage != 20 {
					t.Errorf("Unexpected filter: %+v", f)
				}
				return []client.WooProduct{
					{
						ID:           10,
						Name:         "Hoodie",
						Slug:         "hoodie",
						Price:        "49.99",
						RegularPrice: "49.99",
						Status:       "publish",
						Type:         "simple",
						Description:  "<p>Fleece hoodie</p>",
					},
				}, nil
			},
		}

		handler := handleListProducts(mock)
		res, out, err := handler(ctx, nil, ListProductsInput{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error result: %v", res.Content)
		}
		if len(out) != 1 || out[0].Name != "Hoodie" || out[0].Description != "Fleece hoodie" {
			t.Errorf("unexpected output: %+v", out)
		}
	})

	t.Run("Validation Failure: PerPage > 50", func(t *testing.T) {
		handler := handleListProducts(&mockWooClient{})
		res, _, err := handler(ctx, nil, ListProductsInput{PerPage: 100})
		if err != nil {
			t.Fatalf("unexpected protocol error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})

	t.Run("Woo 401 Unauthorized", func(t *testing.T) {
		mock := &mockWooClient{
			listProductsFunc: func(_ context.Context, _ client.ListProductsFilter) ([]client.WooProduct, error) {
				return nil, &client.APIError{StatusCode: 401, Code: "unauthorized", Message: "Invalid credentials"}
			},
		}
		handler := handleListProducts(mock)
		res, _, err := handler(ctx, nil, ListProductsInput{})
		if err != nil {
			t.Fatalf("unexpected protocol error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if text != "WooCommerce API error (401): Invalid credentials" {
			t.Errorf("unexpected error text: %s", text)
		}
	})

	t.Run("Woo 5xx Error", func(t *testing.T) {
		mock := &mockWooClient{
			listProductsFunc: func(_ context.Context, _ client.ListProductsFilter) ([]client.WooProduct, error) {
				return nil, &client.APIError{StatusCode: 500, Message: "Internal Server Error"}
			},
		}
		handler := handleListProducts(mock)
		res, _, err := handler(ctx, nil, ListProductsInput{})
		if err != nil {
			t.Fatalf("unexpected protocol error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})

	t.Run("Timeout", func(t *testing.T) {
		mock := &mockWooClient{
			listProductsFunc: func(_ context.Context, _ client.ListProductsFilter) ([]client.WooProduct, error) {
				return nil, context.DeadlineExceeded
			},
		}
		handler := handleListProducts(mock)
		res, _, err := handler(ctx, nil, ListProductsInput{})
		if err != nil {
			t.Fatalf("unexpected protocol error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if text != "WooCommerce request timed out" {
			t.Errorf("unexpected timeout text: %s", text)
		}
	})
}

func TestGetProduct(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock := &mockWooClient{
			getProductFunc: func(_ context.Context, id int) (*client.WooProduct, error) {
				if id != 10 {
					t.Errorf("id = %d, want 10", id)
				}
				return &client.WooProduct{
					ID:           10,
					Name:         "Hoodie",
					Slug:         "hoodie",
					Price:        "49.99",
					RegularPrice: "49.99",
					Status:       "publish",
					Type:         "simple",
					StockStatus:  "instock",
					Categories:   []client.WooCategory{{ID: 1, Name: "Apparel"}},
				}, nil
			},
		}

		handler := handleGetProduct(mock)
		res, out, err := handler(ctx, nil, GetProductInput{ID: 10})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error result: %v", res.Content)
		}
		if out.ID != 10 || len(out.Categories) != 1 || out.Categories[0] != "Apparel" {
			t.Errorf("unexpected output: %+v", out)
		}
	})

	t.Run("Validation Failure: ID <= 0", func(t *testing.T) {
		handler := handleGetProduct(&mockWooClient{})
		res, _, _ := handler(ctx, nil, GetProductInput{ID: 0})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})

	t.Run("Woo 404 Not Found", func(t *testing.T) {
		mock := &mockWooClient{
			getProductFunc: func(_ context.Context, _ int) (*client.WooProduct, error) {
				return nil, &client.APIError{StatusCode: 404, Message: "Product not found"}
			},
		}
		handler := handleGetProduct(mock)
		res, _, _ := handler(ctx, nil, GetProductInput{ID: 999})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})
}

func TestCreateProduct(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock := &mockWooClient{
			createProductFunc: func(_ context.Context, req client.CreateProductRequest) (*client.WooProduct, error) {
				if req.Name != "Mug" || req.RegularPrice != "12.50" || req.Status != "publish" || req.Type != "simple" {
					t.Errorf("unexpected create req: %+v", req)
				}
				return &client.WooProduct{
					ID:           15,
					Name:         req.Name,
					Price:        req.RegularPrice,
					RegularPrice: req.RegularPrice,
					Status:       req.Status,
					Type:         req.Type,
					Permalink:    "http://store/product/mug",
				}, nil
			},
		}

		handler := handleCreateProduct(mock)
		res, out, err := handler(ctx, nil, CreateProductInput{
			Name:         "Mug",
			RegularPrice: "12.50",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %v", res.Content)
		}
		if out.ID != 15 || out.Price != "12.50" {
			t.Errorf("unexpected output: %+v", out)
		}
	})

	t.Run("Validation Failure: Missing Name", func(t *testing.T) {
		handler := handleCreateProduct(&mockWooClient{})
		res, _, _ := handler(ctx, nil, CreateProductInput{Name: "", RegularPrice: "10.00"})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})

	t.Run("Validation Failure: Invalid RegularPrice", func(t *testing.T) {
		handler := handleCreateProduct(&mockWooClient{})
		invalidPrices := []string{"abc", "-10", "12.345", ""}
		for _, p := range invalidPrices {
			res, _, _ := handler(ctx, nil, CreateProductInput{Name: "Item", RegularPrice: p})
			if !res.IsError {
				t.Fatalf("expected IsError=true for price %q", p)
			}
		}
	})

	t.Run("Validation Failure: Invalid Status", func(t *testing.T) {
		handler := handleCreateProduct(&mockWooClient{})
		res, _, _ := handler(ctx, nil, CreateProductInput{Name: "Item", RegularPrice: "10.00", Status: "invalid-status"})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})
}

func TestListOrders(t *testing.T) {
	ctx := context.Background()

	t.Run("Success with Date Filter", func(t *testing.T) {
		mock := &mockWooClient{
			listOrdersFunc: func(_ context.Context, f client.ListOrdersFilter) ([]client.WooOrder, error) {
				if f.After == nil {
					t.Fatal("expected After filter to be set")
				}
				return []client.WooOrder{
					{
						ID:                 14,
						Status:             "pending",
						Total:              "49.99",
						Currency:           "USD",
						DateCreatedGMT:     "2026-10-25T07:14:25",
						PaymentMethodTitle: "Cash on delivery",
						Billing: client.WooAddress{
							FirstName: "Secret",
							LastName:  "Person",
							Email:     "secret@example.com",
							Phone:     "123-456-7890",
							Address1:  "123 Private St",
						},
						LineItems: []client.WooLineItem{
							{Name: "Hoodie", Quantity: 1, Total: "49.99"},
						},
					},
				}, nil
			},
		}

		handler := handleListOrders(mock)
		res, out, err := handler(ctx, nil, ListOrdersInput{
			After: "2026-10-25T00:00:00Z",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %v", res.Content)
		}
		if len(out) != 1 || out[0].ID != 14 {
			t.Errorf("unexpected output: %+v", out)
		}
		// Confirm PII is NOT present in OrderSummary and date is RFC3339 with Z suffix
		jsonBytes, _ := json.Marshal(out[0])
		if string(jsonBytes) != `{"id":14,"status":"pending","total":"49.99","currency":"USD","date_created_gmt":"2026-10-25T07:14:25Z","payment_method_title":"Cash on delivery","line_items":[{"name":"Hoodie","quantity":1,"total":"49.99"}]}` {
			t.Errorf("order summary contains unexpected fields: %s", string(jsonBytes))
		}
	})

	t.Run("Validation Failure: Invalid Date Format", func(t *testing.T) {
		handler := handleListOrders(&mockWooClient{})
		res, _, _ := handler(ctx, nil, ListOrdersInput{After: "not-a-date"})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})

	t.Run("Validation Failure: Invalid Status", func(t *testing.T) {
		handler := handleListOrders(&mockWooClient{})
		res, _, _ := handler(ctx, nil, ListOrdersInput{Status: "bad-status"})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})
}

func TestGetOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("Success with Limited PII", func(t *testing.T) {
		mock := &mockWooClient{
			getOrderFunc: func(_ context.Context, id int) (*client.WooOrder, error) {
				return &client.WooOrder{
					ID:                 id,
					Status:             "pending",
					Total:              "49.99",
					Currency:           "USD",
					DateCreatedGMT:     "2026-10-25T07:14:25",
					PaymentMethodTitle: "Cash on delivery",
					Billing: client.WooAddress{
						FirstName: "Jane",
						LastName:  "Doe",
						Email:     "jane@example.com",
						Phone:     "555-123-4567",     // Should be omitted
						Address1:  "456 Market St",    // Should be omitted
					},
					LineItems: []client.WooLineItem{
						{ProductID: 10, Name: "Hoodie", Quantity: 1, Subtotal: "49.99", Total: "49.99"},
					},
				}, nil
			},
		}

		handler := handleGetOrder(mock)
		res, out, err := handler(ctx, nil, GetOrderInput{ID: 14})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %v", res.Content)
		}
		if out.DateCreatedGMT != "2026-10-25T07:14:25Z" {
			t.Errorf("expected date_created_gmt with Z suffix, got: %s", out.DateCreatedGMT)
		}
		if out.Customer.FirstName != "Jane" || out.Customer.LastName != "Doe" || out.Customer.Email != "jane@example.com" {
			t.Errorf("customer details wrong: %+v", out.Customer)
		}
	})

	t.Run("Validation Failure: ID <= 0", func(t *testing.T) {
		handler := handleGetOrder(&mockWooClient{})
		res, _, _ := handler(ctx, nil, GetOrderInput{ID: -1})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})
}

func TestUpdateOrderStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mock := &mockWooClient{
			updateOrderStatusFunc: func(_ context.Context, id int, status string) (*client.WooOrder, error) {
				return &client.WooOrder{
					ID:              id,
					Status:          status,
					DateModifiedGMT: "2026-10-25T08:00:00",
				}, nil
			},
		}

		handler := handleUpdateOrderStatus(mock)
		res, out, err := handler(ctx, nil, UpdateOrderStatusInput{ID: 14, Status: "completed"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.IsError {
			t.Fatalf("expected success, got error: %v", res.Content)
		}
		if out.Status != "completed" || out.ID != 14 {
			t.Errorf("unexpected output: %+v", out)
		}
		if out.DateModified != "2026-10-25T08:00:00Z" {
			t.Errorf("expected RFC3339 with Z, got: %s", out.DateModified)
		}
	})

	t.Run("Validation Failure: Invalid Status", func(t *testing.T) {
		handler := handleUpdateOrderStatus(&mockWooClient{})
		res, _, _ := handler(ctx, nil, UpdateOrderStatusInput{ID: 14, Status: "bogus-status"})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})

	t.Run("Validation Failure: Disallowed Enum Values (trash and refunded)", func(t *testing.T) {
		handler := handleUpdateOrderStatus(&mockWooClient{})
		for _, disallowed := range []string{"trash", "refunded"} {
			res, _, _ := handler(ctx, nil, UpdateOrderStatusInput{ID: 14, Status: disallowed})
			if !res.IsError {
				t.Fatalf("expected status %q to be rejected, got IsError=false", disallowed)
			}
		}
	})

	t.Run("Woo 5xx", func(t *testing.T) {
		mock := &mockWooClient{
			updateOrderStatusFunc: func(_ context.Context, _ int, _ string) (*client.WooOrder, error) {
				return nil, errors.New("database locked")
			},
		}
		handler := handleUpdateOrderStatus(mock)
		res, _, _ := handler(ctx, nil, UpdateOrderStatusInput{ID: 14, Status: "completed"})
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
	})
}

func TestErrorSanitization(t *testing.T) {
	ctx := context.Background()

	t.Run("Network error does not leak URL, hostname, port, or IP", func(t *testing.T) {
		mock := &mockWooClient{
			listProductsFunc: func(_ context.Context, _ client.ListProductsFilter) ([]client.WooProduct, error) {
				return nil, errors.New(`Get "http://store-wordpress.tollbooth.svc.cluster.local:80/wp-json/wc/v3/products": dial tcp 10.96.245.126:80: connect: connection refused`)
			},
		}

		handler := handleListProducts(mock)
		res, _, err := handler(ctx, nil, ListProductsInput{})
		if err != nil {
			t.Fatalf("unexpected go error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
		if len(res.Content) == 0 {
			t.Fatal("expected non-empty content")
		}
		textContent, ok := res.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got: %T", res.Content[0])
		}
		msg := textContent.Text

		forbidden := []string{
			"10.96.245.126",
			"store-wordpress",
			"tollbooth.svc",
			"cluster.local",
			"http://",
			":80",
		}
		for _, f := range forbidden {
			if strings.Contains(msg, f) {
				t.Fatalf("sanitized error leaked forbidden sensitive text %q: full msg: %q", f, msg)
			}
		}
	})

	t.Run("API error sanitizes Message containing IP or host", func(t *testing.T) {
		mock := &mockWooClient{
			getProductFunc: func(_ context.Context, _ int) (*client.WooProduct, error) {
				return nil, &client.APIError{
					StatusCode: 502,
					Message:    "Failed upstream connection to 192.168.1.100:8000 via http://internal-server.local",
				}
			},
		}

		handler := handleGetProduct(mock)
		res, _, err := handler(ctx, nil, GetProductInput{ID: 10})
		if err != nil {
			t.Fatalf("unexpected go error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected IsError=true")
		}
		textContent, ok := res.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got: %T", res.Content[0])
		}
		msg := textContent.Text

		forbidden := []string{
			"192.168.1.100",
			"http://internal-server.local",
		}
		for _, f := range forbidden {
			if strings.Contains(msg, f) {
				t.Fatalf("API error leaked forbidden sensitive text %q: full msg: %q", f, msg)
			}
		}
	})
}
