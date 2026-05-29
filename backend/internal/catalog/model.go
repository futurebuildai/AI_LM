// Package catalog manages per-product physical dimensions (L/W/H, stacking)
// keyed by GableLBM product id. GableLBM owns product weight; AI_LM stores the
// dimensional data the ERP lacks, with an optional weight override.
package catalog

import "time"

// Dimension holds the load-planning attributes for one GableLBM product.
type Dimension struct {
	ID                string    `json:"id"`
	GableProductID    string    `json:"gable_product_id"`
	SKU               string    `json:"sku"`
	LengthIn          float64   `json:"length_in"`
	WidthIn           float64   `json:"width_in"`
	HeightIn          float64   `json:"height_in"`
	Stackable         bool      `json:"stackable"`
	WeightLbsOverride *float64  `json:"weight_lbs_override,omitempty"`
	DefaultSource     string    `json:"default_source"` // UOM/MANUAL/IMPORT
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// DimensionInput is the upsert payload for product dimensions.
type DimensionInput struct {
	SKU               string   `json:"sku"`
	LengthIn          float64  `json:"length_in"`
	WidthIn           float64  `json:"width_in"`
	HeightIn          float64  `json:"height_in"`
	Stackable         bool     `json:"stackable"`
	WeightLbsOverride *float64 `json:"weight_lbs_override,omitempty"`
	DefaultSource     string   `json:"default_source"`
}
