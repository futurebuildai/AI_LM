// Package load builds a 3D load plan for a truck: it places order line items
// on the bed, distributes their weight across axles, and flags GVW compliance.
// The placement algorithm sits behind a Solver interface so a smarter optimizer
// (or ML model) can replace the deterministic MVP heuristic later.
package load

import "time"

// Item is one product line to be loaded (already resolved to dimensions/weight).
type Item struct {
	ProductID string  `json:"product_id"`
	SKU       string  `json:"sku"`
	Quantity  int     `json:"quantity"`
	LengthIn  float64 `json:"length_in"`
	WidthIn   float64 `json:"width_in"`
	HeightIn  float64 `json:"height_in"`
	WeightLbs float64 `json:"weight_lbs"` // per-unit weight
	Stackable bool    `json:"stackable"`
}

// Vehicle is the subset of a fleet profile the solver needs.
type Vehicle struct {
	GableVehicleID string
	BedLengthIn    float64
	BedWidthIn     float64
	BedHeightIn    float64
	GVWRLbs        int64
	TareWeightLbs  int64
	Axles          []Axle
}

// Axle is a rated axle at a longitudinal position.
type Axle struct {
	AxleNumber          int
	MaxWeightLbs        int64
	PositionFromFrontIn float64
	AxleType            string
}

// Placement is one positioned unit box in the 3D scene. Coordinates are inches
// from the front-left-floor corner of the bed: X = length (front→back),
// Y = width (left→right), Z = height (floor→up).
//
// For sequenced (multi-stop) plans, OrderID/StopSequence tie the unit to its
// delivery stop and Step is its 1-based position in the physical packing order
// (step 1 is loaded first, at the nose; the first stop's material is loaded
// last so it comes off first).
type Placement struct {
	ItemID    string  `json:"item_id"`
	SKU       string  `json:"sku"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	LengthIn  float64 `json:"length_in"`
	WidthIn   float64 `json:"width_in"`
	HeightIn  float64 `json:"height_in"`
	WeightLbs float64 `json:"weight_lbs"`
	AxleGroup int     `json:"axle_group"` // nearest axle number, for color coding

	OrderID      string `json:"order_id,omitempty"`
	StopSequence int    `json:"stop_sequence,omitempty"`
	Step         int    `json:"step,omitempty"`
}

// AxleLoad is the computed load on one axle vs its rating.
type AxleLoad struct {
	AxleNumber   int     `json:"axle_number"`
	WeightLbs    int64   `json:"weight_lbs"`
	MaxWeightLbs int64   `json:"max_weight_lbs"`
	Utilization  float64 `json:"utilization"` // weight / max
	Status       string  `json:"status"`      // PASS/WARN/FAIL
}

// Plan is the full solver output, persisted and returned to the 3D view.
type Plan struct {
	ID              string      `json:"id"`
	GableRouteID    *string     `json:"gable_route_id,omitempty"`
	GableDeliveryID *string     `json:"gable_delivery_id,omitempty"`
	GableVehicleID  string      `json:"gable_vehicle_id"`
	Placements      []Placement `json:"placements"`
	TotalWeightLbs  int64       `json:"total_weight_lbs"`
	AxleLoads       []AxleLoad  `json:"axle_loads"`
	BalanceScore    float64     `json:"balance_score"` // 0..1, higher = better
	GVWStatus       string      `json:"gvw_status"`    // PASS/WARN/FAIL
	Unplaced        []string    `json:"unplaced"`      // SKUs that did not fit
	MaxLoadHeightIn float64     `json:"max_load_height_in,omitempty"`
	Securement      *Securement `json:"securement,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
}

// StopItems is one delivery stop's resolved line items for sequenced packing.
type StopItems struct {
	OrderID      string `json:"order_id"`
	StopSequence int    `json:"stop_sequence"`
	Items        []Item `json:"items"`
}

// Strap is one tie-down across the load at a longitudinal position.
type Strap struct {
	Number         int     `json:"number"`
	PositionIn     float64 `json:"position_in"`      // inches from the bed front
	OverHeightIn   float64 `json:"over_height_in"`   // load height under the strap
	RequiredWLLLbs int64   `json:"required_wll_lbs"` // working-load-limit share
}

// Securement is the tie-down plan for a packed load, derived from the
// FMCSA §393.106 / NSC Standard 10 rules: aggregate working load limit ≥ 50%
// of cargo weight, two tie-downs for the first 10 ft of article length plus
// one per additional 10 ft (or fraction).
type Securement struct {
	CargoWeightLbs     int64    `json:"cargo_weight_lbs"`
	MinAggregateWLLLbs int64    `json:"min_aggregate_wll_lbs"`
	Straps             []Strap  `json:"straps"`
	RecommendedStrap   string   `json:"recommended_strap"`
	Notes              []string `json:"notes"`
}
