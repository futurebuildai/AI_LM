package workflow

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/futurebuildai/ai-lm/internal/catalog"
	"github.com/futurebuildai/ai-lm/internal/compliance"
	"github.com/futurebuildai/ai-lm/internal/fleet"
	"github.com/futurebuildai/ai-lm/internal/gable"
	"github.com/futurebuildai/ai-lm/internal/load"
	"github.com/futurebuildai/ai-lm/internal/routing"
)

// Default depot: the Gable Lumber & Supply yard (Kelowna). Used when the
// ingest request does not supply one and no order centroid is available.
const (
	defaultDepotLat = 49.8863
	defaultDepotLng = -119.4666
)

// defaultDeckHeightIn approximates deck height above road for clearance checks:
// total vehicle height = deck + tallest placement.
const defaultDeckHeightIn = 58.0

// gableSource is the GableLBM integration surface the workflow consumes
// (satisfied by *gable.Client).
type gableSource interface {
	ListOrdersForDate(ctx context.Context, date string) ([]gable.Order, error)
	ListVehicles(ctx context.Context) ([]gable.Vehicle, error)
	ListDrivers(ctx context.Context) ([]gable.Driver, error)
	PushDeliveryRoute(ctx context.Context, route gable.DeliveryRoute) error
	SeedDemoOrders(ctx context.Context, date string) (*gable.SeedResult, error)
}

// catalogSource resolves products to effective geometry (satisfied by *catalog.Service).
type catalogSource interface {
	ListEffectiveProducts(ctx context.Context) ([]catalog.EffectiveProduct, error)
}

// fleetProfiles supplies and auto-provisions vehicle profiles (satisfied by *fleet.Service).
type fleetProfiles interface {
	GetProfile(ctx context.Context, gableVehicleID string) (*fleet.Profile, error)
	UpsertProfile(ctx context.Context, gableVehicleID string, in fleet.ProfileInput) (*fleet.Profile, error)
}

// routeChecker runs restricted-point checks (satisfied by *compliance.Service).
type routeChecker interface {
	CheckRoute(ctx context.Context, req compliance.RouteCheckRequest) (*compliance.RouteCheckResult, error)
}

// Service orchestrates the five-step dispatch workflow.
type Service struct {
	repo    *Repository
	gable   gableSource
	catalog catalogSource
	fleet   fleetProfiles
	checker routeChecker
}

func NewService(repo *Repository, g gableSource, c catalogSource, f fleetProfiles, rc routeChecker) *Service {
	return &Service{repo: repo, gable: g, catalog: c, fleet: f, checker: rc}
}

// Get returns a plan by id.
func (s *Service) Get(ctx context.Context, id string) (*Plan, error) {
	return s.repo.Get(ctx, id)
}

// GetLatestForDate returns the most recent plan for a date.
func (s *Service) GetLatestForDate(ctx context.Context, date string) (*Plan, error) {
	return s.repo.GetLatestForDate(ctx, date)
}

// DemoSeed proxies to GableLBM's demo-seed endpoint (next-day lumber orders +
// digital-twin dims for the lumber SKUs).
func (s *Service) DemoSeed(ctx context.Context, date string) (*gable.SeedResult, error) {
	return s.gable.SeedDemoOrders(ctx, date)
}

// --- Step 1+2: ingest + deep analysis ---------------------------------------

// Ingest pulls every confirmed order scheduled for the date and analyzes each
// one: per-line effective geometry/weight, totals, shape profile, issues.
func (s *Service) Ingest(ctx context.Context, req IngestRequest) (*Plan, error) {
	if req.Date == "" {
		return nil, fmt.Errorf("date is required")
	}
	if _, err := time.Parse("2006-01-02", req.Date); err != nil {
		return nil, fmt.Errorf("invalid date %q; expected YYYY-MM-DD", req.Date)
	}

	orders, err := s.gable.ListOrdersForDate(ctx, req.Date)
	if err != nil {
		return nil, fmt.Errorf("fetch orders: %w", err)
	}

	products, err := s.catalog.ListEffectiveProducts(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve catalog: %w", err)
	}
	byProduct := make(map[string]catalog.EffectiveProduct, len(products))
	for _, p := range products {
		byProduct[p.GableProductID] = p
	}

	analyses := make([]OrderAnalysis, 0, len(orders))
	var sumLat, sumLng float64
	geoCount := 0
	for _, o := range orders {
		a := analyzeOrder(o, byProduct)
		if a.Routable {
			sumLat += *a.Lat
			sumLng += *a.Lng
			geoCount++
		}
		analyses = append(analyses, a)
	}

	depotLat, depotLng := defaultDepotLat, defaultDepotLng
	if req.DepotLat != nil && req.DepotLng != nil {
		depotLat, depotLng = *req.DepotLat, *req.DepotLng
	}

	plan := &Plan{
		PlanDate:         req.Date,
		Status:           StatusAnalyzed,
		DepotLat:         depotLat,
		DepotLng:         depotLng,
		Orders:           analyses,
		Loads:            []TruckLoad{},
		UnassignedOrders: []Stop{},
	}
	if err := s.repo.Create(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// analyzeOrder resolves one order's lines against the effective catalog and
// derives weight/volume/shape metrics.
func analyzeOrder(o gable.Order, byProduct map[string]catalog.EffectiveProduct) OrderAnalysis {
	a := OrderAnalysis{
		OrderID:      o.ID,
		CustomerName: o.CustomerName,
		Address:      o.Address,
		Lat:          o.Latitude,
		Lng:          o.Longitude,
		Lines:        []AnalyzedLine{},
		Issues:       []string{},
		Routable:     o.Latitude != nil && o.Longitude != nil,
	}
	if !a.Routable {
		a.Issues = append(a.Issues, "no delivery geolocation — cannot route")
	}

	missingGeometry := 0
	for _, l := range o.Lines {
		line := AnalyzedLine{
			ProductID:     l.ProductID,
			SKU:           l.SKU,
			Quantity:      l.Quantity,
			UnitWeightLbs: l.WeightLbs,
		}
		if ep, ok := byProduct[l.ProductID]; ok {
			line.Name = ep.Name
			line.UnitLengthIn = ep.LengthIn
			line.UnitWidthIn = ep.WidthIn
			line.UnitHeightIn = ep.HeightIn
			line.Stackable = ep.Stackable
			line.HasGeometry = ep.HasGeometry
			if ep.WeightLbs > 0 {
				line.UnitWeightLbs = ep.WeightLbs
			}
		}
		if !line.HasGeometry {
			missingGeometry++
		}
		line.LineWeightLbs = round2(line.UnitWeightLbs * l.Quantity)
		line.LineVolumeCuFt = round2(line.UnitLengthIn * line.UnitWidthIn * line.UnitHeightIn / 1728.0 * l.Quantity)

		a.Lines = append(a.Lines, line)
		a.TotalWeightLbs += line.LineWeightLbs
		a.TotalVolumeCuFt += line.LineVolumeCuFt
		a.PieceCount += int(math.Round(l.Quantity))
		if line.UnitLengthIn > a.MaxLengthIn {
			a.MaxLengthIn = line.UnitLengthIn
		}
	}
	a.TotalWeightLbs = round2(a.TotalWeightLbs)
	a.TotalVolumeCuFt = round2(a.TotalVolumeCuFt)
	if missingGeometry > 0 {
		a.Issues = append(a.Issues, fmt.Sprintf("%d line(s) missing digital-twin geometry", missingGeometry))
	}

	switch {
	case a.MaxLengthIn >= 192:
		a.ShapeProfile = ShapeLongLoad
	case a.MaxLengthIn > 0 && a.MaxLengthIn <= 96:
		a.ShapeProfile = ShapeCompact
	default:
		a.ShapeProfile = ShapeMixed
	}
	return a
}

// --- Step 3: assign orders to trucks + sequence routes -----------------------

// Assign splits the analyzed orders across the live fleet (CVRP by weight) and
// sequences each truck's route from the depot.
func (s *Service) Assign(ctx context.Context, id string) (*Plan, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	byOrder := orderIndex(p)
	var rstops []routing.Stop
	for _, a := range p.Orders {
		if !a.Routable {
			continue
		}
		rstops = append(rstops, routing.Stop{
			OrderID:   a.OrderID,
			Lat:       *a.Lat,
			Lng:       *a.Lng,
			Address:   a.Address,
			WeightLbs: a.TotalWeightLbs,
		})
	}

	vehicles, err := s.gable.ListVehicles(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch vehicles: %w", err)
	}
	drivers, err := s.gable.ListDrivers(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch drivers: %w", err)
	}

	rloads, unassigned := sweepAssign(vehicles, rstops, p.DepotLat, p.DepotLng)
	routing.AssignDrivers(drivers, rloads)

	p.Loads = make([]TruckLoad, 0, len(rloads))
	for _, rl := range rloads {
		ordered, dist, dur := routing.OptimizeSequence(p.DepotLat, p.DepotLng, rl.Stops)
		tl := TruckLoad{
			VehicleID:         rl.VehicleID,
			VehicleName:       rl.VehicleName,
			DriverID:          rl.DriverID,
			DriverName:        rl.DriverName,
			CapacityWeightLbs: rl.CapacityWeightLbs,
			TotalWeightLbs:    round2(rl.TotalWeightLbs),
			TotalDistanceMi:   dist,
			TotalDurationMin:  dur,
			Stops:             make([]Stop, 0, len(ordered)),
		}
		for _, st := range ordered {
			tl.Stops = append(tl.Stops, toWorkflowStop(st, byOrder))
		}
		p.Loads = append(p.Loads, tl)
	}

	p.UnassignedOrders = make([]Stop, 0, len(unassigned))
	for _, st := range unassigned {
		p.UnassignedOrders = append(p.UnassignedOrders, toWorkflowStop(st, byOrder))
	}

	p.Status = StatusAssigned
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// --- Step 4: pack every truck (LIFO bundles) ---------------------------------

// Pack 3D-packs every assigned truck: stops load in reverse route order so the
// first delivery is the last material on (rear of bed, first off).
func (s *Service) Pack(ctx context.Context, id string) (*Plan, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(p.Loads) == 0 {
		return nil, fmt.Errorf("no truck assignments yet — run assign first")
	}

	vehicles, err := s.gable.ListVehicles(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch vehicles: %w", err)
	}
	vehiclesByID := make(map[string]gable.Vehicle, len(vehicles))
	for _, v := range vehicles {
		vehiclesByID[v.ID] = v
	}

	for i := range p.Loads {
		if err := s.packLoad(ctx, p, &p.Loads[i], vehiclesByID, 0); err != nil {
			return nil, err
		}
	}

	p.Status = StatusPacked
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// packLoad solves one truck's 3D placement. maxHeightIn > 0 caps the load
// height below the bed envelope (used by compliance load adjustment under a
// low-clearance route).
func (s *Service) packLoad(ctx context.Context, p *Plan, l *TruckLoad, vehiclesByID map[string]gable.Vehicle, maxHeightIn float64) error {
	profile, err := s.ensureProfile(ctx, l, vehiclesByID)
	if err != nil {
		return err
	}
	l.Bed = &BedDims{LengthIn: profile.BedLengthIn, WidthIn: profile.BedWidthIn, HeightIn: profile.BedHeightIn}

	byOrder := orderIndex(p)
	stops := make([]load.StopItems, 0, len(l.Stops))
	for _, st := range l.Stops {
		a, ok := byOrder[st.OrderID]
		if !ok {
			continue
		}
		si := load.StopItems{OrderID: st.OrderID, StopSequence: st.Sequence}
		for _, line := range a.Lines {
			si.Items = append(si.Items, load.Item{
				ProductID: line.ProductID,
				SKU:       line.SKU,
				Quantity:  int(math.Round(line.Quantity)),
				LengthIn:  line.UnitLengthIn,
				WidthIn:   line.UnitWidthIn,
				HeightIn:  line.UnitHeightIn,
				WeightLbs: line.UnitWeightLbs,
				Stackable: line.Stackable,
			})
		}
		stops = append(stops, si)
	}

	v := toSolverVehicle(profile)
	if maxHeightIn > 0 && maxHeightIn < v.BedHeightIn {
		v.BedHeightIn = maxHeightIn
	}
	lp := load.SolveSequencedBundles(v, stops)
	l.LoadPlan = &lp
	l.Compliance = nil // packing changed — any previous review is stale
	return nil
}

// Resequence manually reorders one truck's stops (the dispatcher's packing-
// stage adjustment), then re-packs that truck and recomputes its route totals.
func (s *Service) Resequence(ctx context.Context, id, vehicleID string, orderIDs []string) (*Plan, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	var l *TruckLoad
	for i := range p.Loads {
		if p.Loads[i].VehicleID == vehicleID {
			l = &p.Loads[i]
			break
		}
	}
	if l == nil {
		return nil, fmt.Errorf("no load for vehicle %s in this plan", vehicleID)
	}

	byOrder := make(map[string]Stop, len(l.Stops))
	for _, st := range l.Stops {
		byOrder[st.OrderID] = st
	}
	if len(orderIDs) != len(l.Stops) {
		return nil, fmt.Errorf("order_ids must be a permutation of the load's %d stops", len(l.Stops))
	}
	reordered := make([]Stop, 0, len(orderIDs))
	for i, oid := range orderIDs {
		st, ok := byOrder[oid]
		if !ok {
			return nil, fmt.Errorf("order %s is not on this load", oid)
		}
		st.Sequence = i + 1
		reordered = append(reordered, st)
		delete(byOrder, oid)
	}
	l.Stops = reordered
	l.TotalDistanceMi, l.TotalDurationMin = routeTotals(p.DepotLat, p.DepotLng, l.Stops)

	vehicles, err := s.gable.ListVehicles(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch vehicles: %w", err)
	}
	vehiclesByID := make(map[string]gable.Vehicle, len(vehicles))
	for _, v := range vehicles {
		vehiclesByID[v.ID] = v
	}
	if err := s.packLoad(ctx, p, l, vehiclesByID, 0); err != nil {
		return nil, err
	}

	// A manual resequence invalidates any later-stage artifacts.
	if p.Status == StatusReviewed || p.Status == StatusPushed {
		p.Status = StatusPacked
	}
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// --- Step 6: push to the GableLBM dispatch board ------------------------------

// Push writes every truck's route + packing manifest to GableLBM. Blocked while
// any truck still has a FAIL compliance status.
func (s *Service) Push(ctx context.Context, id string) (*Plan, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(p.Loads) == 0 {
		return nil, fmt.Errorf("nothing to push — no truck loads")
	}

	var failing []string
	for _, l := range p.Loads {
		if l.LoadPlan == nil {
			return nil, fmt.Errorf("truck %s is not packed yet", l.VehicleName)
		}
		if l.Compliance == nil {
			return nil, fmt.Errorf("truck %s has not passed route review yet", l.VehicleName)
		}
		if l.Compliance.Status == "FAIL" {
			failing = append(failing, l.VehicleName)
		}
	}
	if len(failing) > 0 {
		return nil, fmt.Errorf("compliance FAIL on: %s — resolve before pushing", strings.Join(failing, ", "))
	}

	for _, l := range p.Loads {
		route := gable.DeliveryRoute{
			VehicleID:     l.VehicleID,
			DriverID:      l.DriverID,
			ScheduledDate: p.PlanDate,
			LoadManifest:  buildManifest(p, l),
		}
		for _, st := range l.Stops {
			route.Stops = append(route.Stops, gable.RouteStop{
				OrderID:  st.OrderID,
				Sequence: st.Sequence,
				Lat:      st.Lat,
				Lng:      st.Lng,
			})
		}
		if err := s.gable.PushDeliveryRoute(ctx, route); err != nil {
			return nil, fmt.Errorf("write back to GableLBM (truck %s): %w", l.VehicleName, err)
		}
	}

	p.Status = StatusPushed
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// buildManifest assembles the yard-facing packing manifest for one truck. It is
// stored verbatim on the GableLBM delivery route and rendered by the yard
// "Pack Trucks" surface.
func buildManifest(p *Plan, l TruckLoad) map[string]any {
	byOrder := orderIndex(p)
	stops := make([]map[string]any, 0, len(l.Stops))
	for _, st := range l.Stops {
		pieceCount := 0
		if a, ok := byOrder[st.OrderID]; ok {
			pieceCount = a.PieceCount
		}
		stops = append(stops, map[string]any{
			"order_id":      st.OrderID,
			"sequence":      st.Sequence,
			"customer_name": st.CustomerName,
			"address":       st.Address,
			"weight_lbs":    st.WeightLbs,
			"piece_count":   pieceCount,
		})
	}
	skuNames := map[string]string{}
	for _, a := range p.Orders {
		for _, line := range a.Lines {
			if line.Name != "" {
				skuNames[line.SKU] = line.Name
			}
		}
	}
	return map[string]any{
		"version":            1,
		"plan_date":          p.PlanDate,
		"vehicle_id":         l.VehicleID,
		"vehicle_name":       l.VehicleName,
		"driver_name":        l.DriverName,
		"bed":                l.Bed,
		"total_weight_lbs":   l.LoadPlan.TotalWeightLbs,
		"gvw_status":         l.LoadPlan.GVWStatus,
		"max_load_height_in": l.LoadPlan.MaxLoadHeightIn,
		"axle_loads":         l.LoadPlan.AxleLoads,
		"stops":              stops,
		"steps":              l.LoadPlan.Placements, // already in pack order with Step set
		"sku_names":          skuNames,
		"securement":         l.LoadPlan.Securement,
		"compliance":         l.Compliance,
	}
}

// --- helpers -----------------------------------------------------------------

func orderIndex(p *Plan) map[string]*OrderAnalysis {
	m := make(map[string]*OrderAnalysis, len(p.Orders))
	for i := range p.Orders {
		m[p.Orders[i].OrderID] = &p.Orders[i]
	}
	return m
}

func toWorkflowStop(st routing.Stop, byOrder map[string]*OrderAnalysis) Stop {
	out := Stop{
		OrderID:   st.OrderID,
		Sequence:  st.Sequence,
		Lat:       st.Lat,
		Lng:       st.Lng,
		Address:   st.Address,
		WeightLbs: round2(st.WeightLbs),
	}
	if a, ok := byOrder[st.OrderID]; ok {
		out.CustomerName = a.CustomerName
	}
	return out
}

// routeTotals computes path distance/duration for a fixed stop order.
func routeTotals(depotLat, depotLng float64, stops []Stop) (float64, float64) {
	const avgSpeedMph = 35.0
	total := 0.0
	pLat, pLng := depotLat, depotLng
	for _, st := range stops {
		total += routing.HaversineMiles(pLat, pLng, st.Lat, st.Lng)
		pLat, pLng = st.Lat, st.Lng
	}
	return round2(total), round2(total / avgSpeedMph * 60.0)
}

// ensureProfile fetches the truck's fleet profile, auto-provisioning a
// sensible default from the GableLBM vehicle type when none exists yet (the
// dispatcher can refine it later on the Fleet page).
func (s *Service) ensureProfile(ctx context.Context, l *TruckLoad, vehiclesByID map[string]gable.Vehicle) (*fleet.Profile, error) {
	profile, err := s.fleet.GetProfile(ctx, l.VehicleID)
	if err == nil {
		return profile, nil
	}
	if err != fleet.ErrNotFound {
		return nil, fmt.Errorf("load fleet profile for %s: %w", l.VehicleName, err)
	}

	v, ok := vehiclesByID[l.VehicleID]
	if !ok {
		v = gable.Vehicle{ID: l.VehicleID, Name: l.VehicleName, VehicleType: "FLATBED"}
	}
	input := defaultProfileInput(v)
	created, err := s.fleet.UpsertProfile(ctx, l.VehicleID, input)
	if err != nil {
		return nil, fmt.Errorf("auto-provision fleet profile for %s: %w", l.VehicleName, err)
	}
	return created, nil
}

// defaultProfileInput derives a load-planning profile from the GableLBM
// vehicle record alone (type + payload capacity).
func defaultProfileInput(v gable.Vehicle) fleet.ProfileInput {
	type spec struct {
		bedL, bedW, bedH float64
		tare             int64
		steer, drive     int64
		drivePos         float64
	}
	sp := spec{bedL: 288, bedW: 96, bedH: 96, tare: 14000, steer: 12000, drive: 21000, drivePos: 240} // flatbed default
	t := strings.ToUpper(v.VehicleType)
	switch {
	case strings.Contains(t, "BOX"):
		sp = spec{bedL: 312, bedW: 100, bedH: 102, tare: 12500, steer: 10000, drive: 17500, drivePos: 260}
	case strings.Contains(t, "PICKUP"):
		sp = spec{bedL: 98, bedW: 64, bedH: 21, tare: 6500, steer: 4800, drive: 6500, drivePos: 160}
	case strings.Contains(t, "VAN"):
		sp = spec{bedL: 144, bedW: 70, bedH: 64, tare: 6000, steer: 4600, drive: 5500, drivePos: 140}
	case strings.Contains(t, "CRANE"):
		sp = spec{bedL: 264, bedW: 96, bedH: 96, tare: 22000, steer: 14000, drive: 23000, drivePos: 230}
	}

	gvwr := sp.tare + 12000
	if v.CapacityWeightLbs != nil && *v.CapacityWeightLbs > 0 {
		gvwr = sp.tare + int64(*v.CapacityWeightLbs)
	}
	name := v.Name
	if name == "" {
		name = v.ID
	}
	return fleet.ProfileInput{
		Name:          name,
		BedLengthIn:   sp.bedL,
		BedWidthIn:    sp.bedW,
		BedHeightIn:   sp.bedH,
		GVWRLbs:       gvwr,
		TareWeightLbs: sp.tare,
		Axles: []fleet.AxleInput{
			{AxleNumber: 1, MaxWeightLbs: sp.steer, PositionFromFrontIn: 0, AxleType: "STEER"},
			{AxleNumber: 2, MaxWeightLbs: sp.drive, PositionFromFrontIn: sp.drivePos, AxleType: "DRIVE"},
		},
	}
}

func toSolverVehicle(p *fleet.Profile) load.Vehicle {
	v := load.Vehicle{
		GableVehicleID: p.GableVehicleID,
		BedLengthIn:    p.BedLengthIn,
		BedWidthIn:     p.BedWidthIn,
		BedHeightIn:    p.BedHeightIn,
		GVWRLbs:        p.GVWRLbs,
		TareWeightLbs:  p.TareWeightLbs,
	}
	for _, a := range p.Axles {
		v.Axles = append(v.Axles, load.Axle{
			AxleNumber:          a.AxleNumber,
			MaxWeightLbs:        a.MaxWeightLbs,
			PositionFromFrontIn: a.PositionFromFrontIn,
			AxleType:            a.AxleType,
		})
	}
	return v
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
