// Package workflow orchestrates the guided end-to-end dispatch flow:
//
//	1. Ingest    — pull a calendar date's confirmed GableLBM orders and deep-
//	               analyze each one (per-line geometry, weight, volume, shape).
//	2. Assign    — split orders across trucks (CVRP) and sequence each route.
//	3. Pack      — 3D-pack every truck LIFO by stop (last stop loads first) as
//	               realistic banded lumber bundles; routes may be resequenced.
//	4. Review    — check every route against restricted points (bridge weight
//	               limits, overpass clearances) and auto-resolve flags by
//	               rerouting or re-balancing loads across trucks.
//	5. Push      — write the final routes + packing manifests to the GableLBM
//	               dispatch board (and its yard Pack Trucks surface).
//
// One Plan row carries the whole run; each step persists its artifacts so the
// UI can resume/replay any stage.
package workflow

import (
	"time"

	"github.com/futurebuildai/ai-lm/internal/compliance"
	"github.com/futurebuildai/ai-lm/internal/load"
)

// Plan statuses, in workflow order.
const (
	StatusAnalyzed = "ANALYZED"
	StatusAssigned = "ASSIGNED"
	StatusPacked   = "PACKED"
	StatusReviewed = "REVIEWED"
	StatusPushed   = "PUSHED"
)

// Shape profiles summarize what kind of load an order is (drives truck choice).
const (
	ShapeLongLoad = "LONG_LOAD" // longest piece ≥ 16 ft
	ShapeCompact  = "COMPACT"   // everything under 8 ft
	ShapeMixed    = "MIXED"
)

// AnalyzedLine is one order line resolved against the effective catalog
// (override → PIM → fallback geometry) with derived per-line totals.
type AnalyzedLine struct {
	ProductID       string  `json:"product_id"`
	SKU             string  `json:"sku"`
	Name            string  `json:"name,omitempty"`
	Quantity        float64 `json:"quantity"`
	UnitWeightLbs   float64 `json:"unit_weight_lbs"`
	UnitLengthIn    float64 `json:"unit_length_in"`
	UnitWidthIn     float64 `json:"unit_width_in"`
	UnitHeightIn    float64 `json:"unit_height_in"`
	Stackable       bool    `json:"stackable"`
	HasGeometry     bool    `json:"has_geometry"`
	LineWeightLbs   float64 `json:"line_weight_lbs"`
	LineVolumeCuFt  float64 `json:"line_volume_cuft"`
}

// OrderAnalysis is the deep analysis of one ingested order.
type OrderAnalysis struct {
	OrderID         string         `json:"order_id"`
	CustomerName    string         `json:"customer_name,omitempty"`
	Address         string         `json:"address,omitempty"`
	Lat             *float64       `json:"lat,omitempty"`
	Lng             *float64       `json:"lng,omitempty"`
	Lines           []AnalyzedLine `json:"lines"`
	TotalWeightLbs  float64        `json:"total_weight_lbs"`
	TotalVolumeCuFt float64        `json:"total_volume_cuft"`
	MaxLengthIn     float64        `json:"max_length_in"`
	PieceCount      int            `json:"piece_count"`
	ShapeProfile    string         `json:"shape_profile"`
	Routable        bool           `json:"routable"`
	Issues          []string       `json:"issues"`
}

// Stop is one sequenced delivery stop on a truck.
type Stop struct {
	OrderID      string  `json:"order_id"`
	Sequence     int     `json:"sequence"`
	Lat          float64 `json:"lat"`
	Lng          float64 `json:"lng"`
	Address      string  `json:"address,omitempty"`
	CustomerName string  `json:"customer_name,omitempty"`
	WeightLbs    float64 `json:"weight_lbs"`
}

// BedDims is the truck bed envelope from the fleet profile (for the 3D view).
type BedDims struct {
	LengthIn float64 `json:"length_in"`
	WidthIn  float64 `json:"width_in"`
	HeightIn float64 `json:"height_in"`
}

// ComplianceAction is one automatic resolution the reviewer applied (or tried).
type ComplianceAction struct {
	Type        string `json:"type"` // REROUTE / LOAD_ADJUST / MANUAL_REVIEW
	Description string `json:"description"`
	Resolved    bool   `json:"resolved"`
}

// ComplianceReview is the restricted-point check outcome for one truck route.
// Detours are AI-inserted waypoints that steer the route polyline around
// flagged restrictions (rendered on the route map; not pushed to GableLBM).
type ComplianceReview struct {
	Status            string                   `json:"status"` // PASS/WARN/FAIL
	Flags             []compliance.Flag        `json:"flags"`
	Actions           []ComplianceAction       `json:"actions"`
	Detours           []compliance.RoutePoint  `json:"detours,omitempty"`
	CheckedGrossLbs   int64                    `json:"checked_gross_lbs"`
	CheckedMaxAxleLbs int64                    `json:"checked_max_axle_lbs"`
	CheckedHeightIn   float64                  `json:"checked_height_in"`
}

// TruckLoad is one truck's full workflow artifact: route, packing, compliance.
type TruckLoad struct {
	VehicleID         string            `json:"vehicle_id"`
	VehicleName       string            `json:"vehicle_name"`
	DriverID          string            `json:"driver_id,omitempty"`
	DriverName        string            `json:"driver_name,omitempty"`
	CapacityWeightLbs int               `json:"capacity_weight_lbs"`
	Stops             []Stop            `json:"stops"`
	TotalWeightLbs    float64           `json:"total_weight_lbs"`
	TotalDistanceMi   float64           `json:"total_distance_mi"`
	TotalDurationMin  float64           `json:"total_duration_min"`
	Bed               *BedDims          `json:"bed,omitempty"`
	LoadPlan          *load.Plan        `json:"load_plan,omitempty"`
	Compliance        *ComplianceReview `json:"compliance,omitempty"`
}

// Plan is one end-to-end workflow run for a delivery date.
type Plan struct {
	ID               string          `json:"id"`
	PlanDate         string          `json:"plan_date"` // YYYY-MM-DD
	Status           string          `json:"status"`
	DepotLat         float64         `json:"depot_lat"`
	DepotLng         float64         `json:"depot_lng"`
	Orders           []OrderAnalysis `json:"orders"`
	Loads            []TruckLoad     `json:"loads"`
	UnassignedOrders []Stop          `json:"unassigned_orders"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// IngestRequest starts a workflow run for a date.
type IngestRequest struct {
	Date     string   `json:"date"` // YYYY-MM-DD
	DepotLat *float64 `json:"depot_lat,omitempty"`
	DepotLng *float64 `json:"depot_lng,omitempty"`
}

// ResequenceRequest manually reorders one truck's stops (triggers a re-pack).
type ResequenceRequest struct {
	OrderIDs []string `json:"order_ids"`
}
