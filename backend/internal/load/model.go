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
	CreatedAt       time.Time   `json:"created_at"`
}
