// Package gable is the HTTP client for GableLBM's /api/integration/* surface.
// AI_LM is a standalone service: it pulls its source-of-truth data (vehicles,
// orders, products+weight) from GableLBM and writes approved routes back, all
// authenticated with the X-Integration-Key header.
package gable

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client talks to a GableLBM instance over the integration API.
type Client struct {
	baseURL        string
	integrationKey string
	http           *http.Client
}

// NewClient builds a GableLBM integration client. baseURL is e.g.
// "http://localhost:8080"; integrationKey is sent as X-Integration-Key.
func NewClient(baseURL, integrationKey string) *Client {
	return &Client{
		baseURL:        baseURL,
		integrationKey: integrationKey,
		http:           &http.Client{Timeout: 15 * time.Second},
	}
}

// --- Wire types (mirror GableLBM integration responses) ---

// Vehicle is a fleet vehicle from GableLBM. Capacity is nullable upstream.
type Vehicle struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	VehicleType       string `json:"vehicle_type"`
	LicensePlate      string `json:"license_plate,omitempty"`
	CapacityWeightLbs *int   `json:"capacity_weight_lbs,omitempty"`
	Make              string `json:"make,omitempty"`
	Model             string `json:"model,omitempty"`
	Year              int    `json:"year,omitempty"`
}

// Product is a catalog product including its per-unit weight.
type Product struct {
	ID       string  `json:"id"`
	SKU      string  `json:"sku"`
	Name     string  `json:"name"`
	Category string  `json:"category,omitempty"`
	UOM      string  `json:"uom,omitempty"`
	WeightLbs float64 `json:"weight_lbs"`
}

// OrderLine is a single line item on an order.
type OrderLine struct {
	ProductID string  `json:"product_id"`
	SKU       string  `json:"sku"`
	Quantity  float64 `json:"quantity"`
	WeightLbs float64 `json:"weight_lbs"`
}

// Order is a confirmed order with optional delivery geolocation.
type Order struct {
	ID        string      `json:"id"`
	Status    string      `json:"status"`
	Address   string      `json:"address,omitempty"`
	Latitude  *float64    `json:"latitude,omitempty"`
	Longitude *float64    `json:"longitude,omitempty"`
	Lines     []OrderLine `json:"lines"`
}

// RouteStop is a single stop in an approved delivery route written back to LBM.
type RouteStop struct {
	OrderID  string  `json:"order_id"`
	Sequence int     `json:"sequence"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
}

// DeliveryRoute is the write-back payload for an approved plan.
type DeliveryRoute struct {
	VehicleID     string      `json:"vehicle_id"`
	DriverID      string      `json:"driver_id,omitempty"`
	ScheduledDate string      `json:"scheduled_date"` // YYYY-MM-DD
	Stops         []RouteStop `json:"stops"`
}

// --- Methods ---

// ListVehicles returns the GableLBM fleet.
func (c *Client) ListVehicles(ctx context.Context) ([]Vehicle, error) {
	var out struct {
		Vehicles []Vehicle `json:"vehicles"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/integration/vehicles", nil, &out); err != nil {
		return nil, err
	}
	return out.Vehicles, nil
}

// GetProductsWithWeight returns the catalog with per-unit weights.
func (c *Client) GetProductsWithWeight(ctx context.Context) ([]Product, error) {
	var out struct {
		Products []Product `json:"products"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/integration/products", nil, &out); err != nil {
		return nil, err
	}
	return out.Products, nil
}

// ListOrdersForDate returns confirmed orders for a scheduled date (YYYY-MM-DD).
func (c *Client) ListOrdersForDate(ctx context.Context, date string) ([]Order, error) {
	q := url.Values{}
	q.Set("date", date)
	q.Set("status", "CONFIRMED")
	var out struct {
		Orders []Order `json:"orders"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/integration/orders?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.Orders, nil
}

// PushDeliveryRoute writes an approved route back to GableLBM. Idempotent
// upstream on (vehicle_id, scheduled_date).
func (c *Client) PushDeliveryRoute(ctx context.Context, route DeliveryRoute) error {
	return c.do(ctx, http.MethodPost, "/api/integration/delivery-routes", route, nil)
}

// do performs a JSON request against the integration API. body may be nil; out
// may be nil when no response decoding is needed.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Integration-Key", c.integrationKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("gable %s %s: status %d: %s", method, path, resp.StatusCode, string(snippet))
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
