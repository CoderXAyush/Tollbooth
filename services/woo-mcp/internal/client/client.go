package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxResponseBytes = 10 << 20 // 10 MB response limit
)

// APIError represents an error response from the WooCommerce REST API.
type APIError struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("WooCommerce API error (%d: %s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("WooCommerce API error (%d): %s", e.StatusCode, e.Message)
}

// Client defines the interface for interacting with WooCommerce.
type Client interface {
	ListProducts(ctx context.Context, filter ListProductsFilter) ([]WooProduct, error)
	GetProduct(ctx context.Context, id int) (*WooProduct, error)
	CreateProduct(ctx context.Context, req CreateProductRequest) (*WooProduct, error)
	ListOrders(ctx context.Context, filter ListOrdersFilter) ([]WooOrder, error)
	GetOrder(ctx context.Context, id int) (*WooOrder, error)
	UpdateOrderStatus(ctx context.Context, id int, status string) (*WooOrder, error)
}

// WooClient implements Client for WooCommerce REST API v3.
type WooClient struct {
	baseURL        string
	consumerKey    string
	consumerSecret string
	httpClient     *http.Client
}

// NewWooClient creates a new WooCommerce REST client.
func NewWooClient(baseURL, consumerKey, consumerSecret string, timeout time.Duration) *WooClient {
	return &WooClient{
		baseURL:        strings.TrimRight(baseURL, "/"),
		consumerKey:    consumerKey,
		consumerSecret: consumerSecret,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// SetHTTPClient allows overriding the http.Client (e.g. for testing).
func (c *WooClient) SetHTTPClient(hc *http.Client) {
	c.httpClient = hc
}

func (c *WooClient) doRequest(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	fullURL := fmt.Sprintf("%s/wp-json/wc/v3%s", c.baseURL, path)
	if len(query) > 0 {
		fullURL = fmt.Sprintf("%s?%s", fullURL, query.Encode())
	}

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.SetBasicAuth(c.consumerKey, c.consumerSecret)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	limitedReader := io.LimitReader(resp.Body, maxResponseBytes)
	respBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if jsonErr := json.Unmarshal(respBytes, &apiErr); jsonErr == nil && apiErr.Message != "" {
			return nil, &APIError{
				StatusCode: resp.StatusCode,
				Code:       apiErr.Code,
				Message:    apiErr.Message,
			}
		}
		// Fallback for non-JSON error responses (e.g. nginx or gateway 502/504)
		msg := strings.TrimSpace(string(respBytes))
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
		}
	}

	return respBytes, nil
}

// ListProducts lists products matching the given filter.
func (c *WooClient) ListProducts(ctx context.Context, filter ListProductsFilter) ([]WooProduct, error) {
	q := url.Values{}
	if filter.Page > 0 {
		q.Set("page", strconv.Itoa(filter.Page))
	} else {
		q.Set("page", "1")
	}
	if filter.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(filter.PerPage))
	} else {
		q.Set("per_page", "20")
	}
	if filter.Search != "" {
		q.Set("search", filter.Search)
	}

	respBytes, err := c.doRequest(ctx, http.MethodGet, "/products", q, nil)
	if err != nil {
		return nil, err
	}

	var products []WooProduct
	if err := json.Unmarshal(respBytes, &products); err != nil {
		return nil, fmt.Errorf("failed to parse products response: %w", err)
	}
	return products, nil
}

// GetProduct retrieves a single product by ID.
func (c *WooClient) GetProduct(ctx context.Context, id int) (*WooProduct, error) {
	if id <= 0 {
		return nil, errors.New("product ID must be a positive integer")
	}

	respBytes, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/products/%d", id), nil, nil)
	if err != nil {
		return nil, err
	}

	var product WooProduct
	if err := json.Unmarshal(respBytes, &product); err != nil {
		return nil, fmt.Errorf("failed to parse product response: %w", err)
	}
	return &product, nil
}

// CreateProduct creates a new product. No retries are performed on POST.
func (c *WooClient) CreateProduct(ctx context.Context, req CreateProductRequest) (*WooProduct, error) {
	respBytes, err := c.doRequest(ctx, http.MethodPost, "/products", nil, req)
	if err != nil {
		return nil, err
	}

	var product WooProduct
	if err := json.Unmarshal(respBytes, &product); err != nil {
		return nil, fmt.Errorf("failed to parse created product response: %w", err)
	}
	return &product, nil
}

// ListOrders lists orders matching the given filter.
// Times are passed with dates_are_gmt=true to ensure exact ISO 8601 UTC evaluation.
func (c *WooClient) ListOrders(ctx context.Context, filter ListOrdersFilter) ([]WooOrder, error) {
	q := url.Values{}
	if filter.Page > 0 {
		q.Set("page", strconv.Itoa(filter.Page))
	} else {
		q.Set("page", "1")
	}
	if filter.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(filter.PerPage))
	} else {
		q.Set("per_page", "20")
	}
	if filter.Status != "" && filter.Status != "any" {
		q.Set("status", filter.Status)
	}
	if filter.After != nil {
		q.Set("after", filter.After.UTC().Format("2006-01-02T15:04:05"))
		q.Set("dates_are_gmt", "true")
	}
	if filter.Before != nil {
		q.Set("before", filter.Before.UTC().Format("2006-01-02T15:04:05"))
		q.Set("dates_are_gmt", "true")
	}

	respBytes, err := c.doRequest(ctx, http.MethodGet, "/orders", q, nil)
	if err != nil {
		return nil, err
	}

	var orders []WooOrder
	if err := json.Unmarshal(respBytes, &orders); err != nil {
		return nil, fmt.Errorf("failed to parse orders response: %w", err)
	}
	return orders, nil
}

// GetOrder retrieves a single order by ID.
func (c *WooClient) GetOrder(ctx context.Context, id int) (*WooOrder, error) {
	if id <= 0 {
		return nil, errors.New("order ID must be a positive integer")
	}

	respBytes, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/orders/%d", id), nil, nil)
	if err != nil {
		return nil, err
	}

	var order WooOrder
	if err := json.Unmarshal(respBytes, &order); err != nil {
		return nil, fmt.Errorf("failed to parse order response: %w", err)
	}
	return &order, nil
}

// UpdateOrderStatus updates an order's status. No retries are performed on PUT.
func (c *WooClient) UpdateOrderStatus(ctx context.Context, id int, status string) (*WooOrder, error) {
	if id <= 0 {
		return nil, errors.New("order ID must be a positive integer")
	}

	req := UpdateOrderStatusRequest{
		Status: status,
	}

	respBytes, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf("/orders/%d", id), nil, req)
	if err != nil {
		return nil, err
	}

	var order WooOrder
	if err := json.Unmarshal(respBytes, &order); err != nil {
		return nil, fmt.Errorf("failed to parse updated order response: %w", err)
	}
	return &order, nil
}
