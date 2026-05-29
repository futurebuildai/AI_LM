// Package routing builds a pre-optimized daily delivery route from confirmed
// GableLBM orders. The MVP optimizer is a deterministic nearest-neighbor + 2-opt
// heuristic over haversine distances; it is pluggable for a real distance-matrix
// provider later. Approved plans are written back to GableLBM.
package routing

import "time"

// Stop is one delivery stop in a route plan.
type Stop struct {
	OrderID   string  `json:"order_id"`
	Sequence  int     `json:"sequence"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Address   string  `json:"address,omitempty"`
	WeightLbs float64 `json:"weight_lbs"`
}

// Plan is a cached, ordered route plan for a date.
type Plan struct {
	ID               string    `json:"id"`
	PlanDate         string    `json:"plan_date"` // YYYY-MM-DD
	GableBranchID    *string   `json:"gable_branch_id,omitempty"`
	GableVehicleID   *string   `json:"gable_vehicle_id,omitempty"`
	Stops            []Stop    `json:"stops"`
	TotalDistanceMi  float64   `json:"total_distance_mi"`
	TotalDurationMin float64   `json:"total_duration_min"`
	Status           string    `json:"status"` // DRAFT/APPROVED
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// PlanRequest asks the optimizer to build a draft plan for a date.
type PlanRequest struct {
	Date      string   `json:"date"` // YYYY-MM-DD
	BranchID  *string  `json:"branch_id,omitempty"`
	VehicleID *string  `json:"vehicle_id,omitempty"`
	DepotLat  *float64 `json:"depot_lat,omitempty"`
	DepotLng  *float64 `json:"depot_lng,omitempty"`
}
