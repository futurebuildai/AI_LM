package routing

import (
	"context"
	"fmt"

	"github.com/futurebuildai/ai-lm/internal/gable"
)

// orderSource fetches confirmed orders for a date (satisfied by *gable.Client).
type orderSource interface {
	ListOrdersForDate(ctx context.Context, date string) ([]gable.Order, error)
}

// routeSink writes an approved route back to GableLBM (satisfied by *gable.Client).
type routeSink interface {
	PushDeliveryRoute(ctx context.Context, route gable.DeliveryRoute) error
}

// Service orchestrates route planning and write-back.
type Service struct {
	repo   *Repository
	orders orderSource
	sink   routeSink
}

func NewService(repo *Repository, orders orderSource, sink routeSink) *Service {
	return &Service{repo: repo, orders: orders, sink: sink}
}

// Plan pulls confirmed orders for the date, optimizes the stop sequence, and
// persists a DRAFT plan for dispatcher fine-tuning.
func (s *Service) Plan(ctx context.Context, req PlanRequest) (*Plan, error) {
	if req.Date == "" {
		return nil, fmt.Errorf("date is required")
	}

	orders, err := s.orders.ListOrdersForDate(ctx, req.Date)
	if err != nil {
		return nil, fmt.Errorf("fetch orders: %w", err)
	}

	// Build stops from orders that carry geolocation.
	var stops []Stop
	var sumLat, sumLng float64
	for _, o := range orders {
		if o.Latitude == nil || o.Longitude == nil {
			continue
		}
		var weight float64
		for _, l := range o.Lines {
			weight += l.WeightLbs * l.Quantity
		}
		stops = append(stops, Stop{
			OrderID:   o.ID,
			Lat:       *o.Latitude,
			Lng:       *o.Longitude,
			Address:   o.Address,
			WeightLbs: weight,
		})
		sumLat += *o.Latitude
		sumLng += *o.Longitude
	}

	// Depot defaults to the centroid of all stops when not supplied.
	depotLat, depotLng := 0.0, 0.0
	if req.DepotLat != nil && req.DepotLng != nil {
		depotLat, depotLng = *req.DepotLat, *req.DepotLng
	} else if len(stops) > 0 {
		depotLat = sumLat / float64(len(stops))
		depotLng = sumLng / float64(len(stops))
	}

	ordered, distance, duration := optimizeSequence(depotLat, depotLng, stops)
	if ordered == nil {
		ordered = []Stop{}
	}

	plan := &Plan{
		PlanDate:         req.Date,
		GableBranchID:    req.BranchID,
		GableVehicleID:   req.VehicleID,
		Stops:            ordered,
		TotalDistanceMi:  distance,
		TotalDurationMin: duration,
		Status:           "DRAFT",
	}
	if err := s.repo.Save(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// Get returns a stored plan by id.
func (s *Service) Get(ctx context.Context, id string) (*Plan, error) {
	return s.repo.Get(ctx, id)
}

// Approve marks a plan APPROVED and writes the route back to GableLBM.
func (s *Service) Approve(ctx context.Context, id string) (*Plan, error) {
	plan, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if plan.GableVehicleID == nil || *plan.GableVehicleID == "" {
		return nil, fmt.Errorf("plan has no vehicle assigned; cannot write back")
	}

	route := gable.DeliveryRoute{
		VehicleID:     *plan.GableVehicleID,
		ScheduledDate: plan.PlanDate,
	}
	for _, st := range plan.Stops {
		route.Stops = append(route.Stops, gable.RouteStop{
			OrderID:  st.OrderID,
			Sequence: st.Sequence,
			Lat:      st.Lat,
			Lng:      st.Lng,
		})
	}

	if err := s.sink.PushDeliveryRoute(ctx, route); err != nil {
		return nil, fmt.Errorf("write back to GableLBM: %w", err)
	}
	if err := s.repo.UpdateStatus(ctx, id, "APPROVED"); err != nil {
		return nil, err
	}
	plan.Status = "APPROVED"
	return plan, nil
}
