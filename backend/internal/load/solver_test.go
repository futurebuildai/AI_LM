package load

import "testing"

func testVehicle() Vehicle {
	return Vehicle{
		GableVehicleID: "veh-1",
		BedLengthIn:    240,
		BedWidthIn:     96,
		BedHeightIn:    96,
		GVWRLbs:        26000,
		TareWeightLbs:  12000,
		Axles: []Axle{
			{AxleNumber: 1, MaxWeightLbs: 12000, PositionFromFrontIn: 0, AxleType: "STEER"},
			{AxleNumber: 2, MaxWeightLbs: 20000, PositionFromFrontIn: 200, AxleType: "DRIVE"},
		},
	}
}

func TestSolvePlacesItemsWithinBed(t *testing.T) {
	s := NewShelfSolver()
	items := []Item{
		{ProductID: "p1", SKU: "2x4", Quantity: 4, LengthIn: 96, WidthIn: 24, HeightIn: 12, WeightLbs: 100, Stackable: true},
	}
	plan := s.Solve(testVehicle(), items)

	if len(plan.Placements) != 4 {
		t.Fatalf("expected 4 placements, got %d", len(plan.Placements))
	}
	if len(plan.Unplaced) != 0 {
		t.Fatalf("expected nothing unplaced, got %v", plan.Unplaced)
	}
	for _, p := range plan.Placements {
		if p.X < 0 || p.X+p.LengthIn > testVehicle().BedLengthIn {
			t.Errorf("placement out of bed length: x=%.1f len=%.1f", p.X, p.LengthIn)
		}
		if p.Y < 0 || p.Y+p.WidthIn > testVehicle().BedWidthIn {
			t.Errorf("placement out of bed width: y=%.1f w=%.1f", p.Y, p.WidthIn)
		}
	}
}

func TestSolveTotalWeightIncludesTare(t *testing.T) {
	s := NewShelfSolver()
	items := []Item{
		{ProductID: "p1", SKU: "block", Quantity: 2, LengthIn: 48, WidthIn: 48, HeightIn: 12, WeightLbs: 500, Stackable: false},
	}
	plan := s.Solve(testVehicle(), items)
	want := int64(12000 + 1000) // tare + 2*500
	if plan.TotalWeightLbs != want {
		t.Fatalf("total weight = %d, want %d", plan.TotalWeightLbs, want)
	}
}

func TestSolveFlagsOverweightGVW(t *testing.T) {
	s := NewShelfSolver()
	// 30 boxes * 1000 lbs = 30000 + 12000 tare = 42000 > 26000 GVWR.
	items := []Item{
		{ProductID: "p1", SKU: "steel", Quantity: 30, LengthIn: 24, WidthIn: 24, HeightIn: 12, WeightLbs: 1000, Stackable: true},
	}
	plan := s.Solve(testVehicle(), items)
	if plan.GVWStatus != "FAIL" {
		t.Fatalf("expected GVW FAIL for overweight load, got %s", plan.GVWStatus)
	}
}

func TestSolveIsDeterministic(t *testing.T) {
	s := NewShelfSolver()
	items := []Item{
		{ProductID: "a", SKU: "a", Quantity: 3, LengthIn: 40, WidthIn: 30, HeightIn: 20, WeightLbs: 200, Stackable: true},
		{ProductID: "b", SKU: "b", Quantity: 2, LengthIn: 50, WidthIn: 40, HeightIn: 20, WeightLbs: 300, Stackable: false},
	}
	v := testVehicle()
	p1 := s.Solve(v, items)
	p2 := s.Solve(v, items)
	if p1.TotalWeightLbs != p2.TotalWeightLbs || p1.BalanceScore != p2.BalanceScore {
		t.Fatalf("solver not deterministic: %+v vs %+v", p1, p2)
	}
	if len(p1.Placements) != len(p2.Placements) {
		t.Fatalf("placement count differs across runs")
	}
}
