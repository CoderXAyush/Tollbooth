package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWooClient_SuccessAndHeaders(t *testing.T) {
	expectedCK := "ck_test123"
	expectedCS := "cs_test456"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check headers
		authHeader := r.Header.Get("Authorization")
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(expectedCK+":"+expectedCS))
		if authHeader != expectedAuth {
			t.Errorf("Authorization header = %q, want %q", authHeader, expectedAuth)
		}
		if r.Header.Get("X-Forwarded-Proto") != "https" {
			t.Errorf("X-Forwarded-Proto = %q, want https", r.Header.Get("X-Forwarded-Proto"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json", r.Header.Get("Accept"))
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]WooProduct{
				{ID: 10, Name: "Test Mug", Price: "12.50", RegularPrice: "12.50", Status: "publish"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products/10":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(WooProduct{
				ID: 10, Name: "Test Mug", Price: "12.50", RegularPrice: "12.50", Status: "publish",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/wp-json/wc/v3/products":
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("POST Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(WooProduct{
				ID: 11, Name: "Test Mug", Price: "12.50", RegularPrice: "12.50", Status: "publish",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/orders":
			if r.URL.Query().Get("dates_are_gmt") != "true" {
				t.Errorf("dates_are_gmt = %q, want true", r.URL.Query().Get("dates_are_gmt"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]WooOrder{
				{ID: 14, Status: "pending", Total: "49.99", Currency: "USD"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/orders/14":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(WooOrder{
				ID: 14, Status: "pending", Total: "49.99", Currency: "USD",
			})
		case r.Method == http.MethodPut && r.URL.Path == "/wp-json/wc/v3/orders/14":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(WooOrder{
				ID: 14, Status: "completed", Total: "49.99", Currency: "USD",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewWooClient(server.URL, expectedCK, expectedCS, 5*time.Second)

	// ListProducts
	products, err := client.ListProducts(context.Background(), ListProductsFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListProducts failed: %v", err)
	}
	if len(products) != 1 || products[0].ID != 10 {
		t.Errorf("ListProducts got %+v", products)
	}

	// GetProduct
	product, err := client.GetProduct(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetProduct failed: %v", err)
	}
	if product.ID != 10 || product.Name != "Test Mug" {
		t.Errorf("GetProduct got %+v", product)
	}

	// CreateProduct
	created, err := client.CreateProduct(context.Background(), CreateProductRequest{
		Name:         "Test Mug",
		RegularPrice: "12.50",
	})
	if err != nil {
		t.Fatalf("CreateProduct failed: %v", err)
	}
	if created.ID != 11 {
		t.Errorf("CreateProduct got %+v", created)
	}

	// ListOrders
	after := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	orders, err := client.ListOrders(context.Background(), ListOrdersFilter{
		Page:    1,
		PerPage: 20,
		After:   &after,
	})
	if err != nil {
		t.Fatalf("ListOrders failed: %v", err)
	}
	if len(orders) != 1 || orders[0].ID != 14 {
		t.Errorf("ListOrders got %+v", orders)
	}

	// GetOrder
	order, err := client.GetOrder(context.Background(), 14)
	if err != nil {
		t.Fatalf("GetOrder failed: %v", err)
	}
	if order.ID != 14 {
		t.Errorf("GetOrder got %+v", order)
	}

	// UpdateOrderStatus
	updated, err := client.UpdateOrderStatus(context.Background(), 14, "completed")
	if err != nil {
		t.Fatalf("UpdateOrderStatus failed: %v", err)
	}
	if updated.Status != "completed" {
		t.Errorf("UpdateOrderStatus status = %s, want completed", updated.Status)
	}
}

func TestWooClient_Errors(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		call       func(c *WooClient) error
		wantStatus int
		wantSubstr string
	}{
		{
			name: "401 Unauthorized",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"woocommerce_rest_cannot_view","message":"Sorry, you cannot list resources.","data":{"status":401}}`))
			},
			call: func(c *WooClient) error {
				_, err := c.ListProducts(context.Background(), ListProductsFilter{})
				return err
			},
			wantStatus: 401,
			wantSubstr: "cannot list resources",
		},
		{
			name: "404 Not Found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"code":"woocommerce_rest_product_invalid_id","message":"Invalid ID.","data":{"status":404}}`))
			},
			call: func(c *WooClient) error {
				_, err := c.GetProduct(context.Background(), 99999)
				return err
			},
			wantStatus: 404,
			wantSubstr: "Invalid ID",
		},
		{
			name: "500 Internal Server Error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"code":"internal_error","message":"Database query failed"}`))
			},
			call: func(c *WooClient) error {
				_, err := c.GetOrder(context.Background(), 10)
				return err
			},
			wantStatus: 500,
			wantSubstr: "Database query failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			c := NewWooClient(server.URL, "key", "secret", 2*time.Second)
			err := tc.call(c)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %T: %v", err, err)
			}
			if apiErr.StatusCode != tc.wantStatus {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.wantStatus)
			}
			if !strings.Contains(apiErr.Message, tc.wantSubstr) {
				t.Errorf("Message %q does not contain %q", apiErr.Message, tc.wantSubstr)
			}
		})
	}
}

func TestWooClient_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewWooClient(server.URL, "key", "secret", 20*time.Millisecond)
	_, err := c.ListProducts(context.Background(), ListProductsFilter{})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
