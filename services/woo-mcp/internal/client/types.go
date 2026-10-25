package client

import "time"

// WooProduct represents the WooCommerce REST API product payload.
type WooProduct struct {
	ID           int               `json:"id"`
	Name         string            `json:"name"`
	Slug         string            `json:"slug"`
	Permalink    string            `json:"permalink"`
	Type         string            `json:"type"`
	Status       string            `json:"status"`
	Description  string            `json:"description"`
	ShortDesc    string            `json:"short_description"`
	Price        string            `json:"price"`
	RegularPrice string            `json:"regular_price"`
	SalePrice    string            `json:"sale_price"`
	StockStatus  string            `json:"stock_status"`
	Categories   []WooCategory     `json:"categories"`
}

type WooCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// WooOrder represents the WooCommerce REST API order payload.
type WooOrder struct {
	ID                 int           `json:"id"`
	ParentID           int           `json:"parent_id"`
	Status             string        `json:"status"`
	Currency           string        `json:"currency"`
	Version            string        `json:"version"`
	DateCreated        string        `json:"date_created"`
	DateCreatedGMT     string        `json:"date_created_gmt"`
	DateModified       string        `json:"date_modified"`
	DateModifiedGMT    string        `json:"date_modified_gmt"`
	DiscountTotal      string        `json:"discount_total"`
	ShippingTotal      string        `json:"shipping_total"`
	Total              string        `json:"total"`
	TotalTax           string        `json:"total_tax"`
	CustomerID         int           `json:"customer_id"`
	OrderKey           string        `json:"order_key"`
	Billing            WooAddress    `json:"billing"`
	Shipping           WooAddress    `json:"shipping"`
	PaymentMethod      string        `json:"payment_method"`
	PaymentMethodTitle string        `json:"payment_method_title"`
	LineItems          []WooLineItem `json:"line_items"`
}

type WooAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company"`
	Address1  string `json:"address_1"`
	Address2  string `json:"address_2"`
	City      string `json:"city"`
	State     string `json:"state"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

type WooLineItem struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ProductID   int    `json:"product_id"`
	VariationID int    `json:"variation_id"`
	Quantity    int    `json:"quantity"`
	Subtotal    string `json:"subtotal"`
	Total       string `json:"total"`
	Price       any    `json:"price"`
}

// CreateProductRequest is the payload sent to create a WooCommerce product.
type CreateProductRequest struct {
	Name         string `json:"name"`
	Type         string `json:"type,omitempty"`
	RegularPrice string `json:"regular_price"`
	Description  string `json:"description,omitempty"`
	Status       string `json:"status,omitempty"`
}

// UpdateOrderStatusRequest is the payload sent to update order status.
type UpdateOrderStatusRequest struct {
	Status string `json:"status"`
}

// ListOrdersFilter specifies parameters for listing orders.
type ListOrdersFilter struct {
	Status  string
	After   *time.Time
	Before  *time.Time
	Page    int
	PerPage int
}

// ListProductsFilter specifies parameters for listing products.
type ListProductsFilter struct {
	Search  string
	Page    int
	PerPage int
}
