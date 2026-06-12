package load

import "testing"

func sequencedTestVehicle() Vehicle {
	return Vehicle{
		GableVehicleID: "veh-1",
		BedLengthIn:    288,
		BedWidthIn:     96,
		BedHeightIn:    96,
		GVWRLbs:        33000,
		TareWeightLbs:  14000,
		Axles: []Axle{
			{AxleNumber: 1, MaxWeightLbs: 12000, PositionFromFrontIn: 0, AxleType: "STEER"},
			{AxleNumber: 2, MaxWeightLbs: 21000, PositionFromFrontIn: 240, AxleType: "DRIVE"},
		},
	}
}

func TestSolveSequencedBundlesLIFO(t *testing.T) {
	stops := []StopItems{
		{OrderID: "first-stop", StopSequence: 1, Items: []Item{
			{ProductID: "p1", SKU: "2x4", Quantity: 20, LengthIn: 96, WidthIn: 3.5, HeightIn: 1.5, WeightLbs: 9, Stackable: true},
		}},
		{OrderID: "last-stop", StopSequence: 2, Items: []Item{
			{ProductID: "p2", SKU: "2x6", Quantity: 20, LengthIn: 120, WidthIn: 5.5, HeightIn: 1.5, WeightLbs: 17, Stackable: true},
		}},
	}

	plan := SolveSequencedBundles(sequencedTestVehicle(), stops)

	if len(plan.Placements) != 40 {
		t.Fatalf("expected 40 placements, got %d (unplaced: %v)", len(plan.Placements), plan.Unplaced)
	}

	// LIFO tiers: the LAST stop packs FIRST (lowest steps, bottom tier); the
	// FIRST stop loads last, on top, where it comes off first.
	if got := plan.Placements[0]; got.OrderID != "last-stop" || got.Step != 1 {
		t.Errorf("first placement should be last-stop step 1, got order=%s step=%d", got.OrderID, got.Step)
	}
	var maxTopLast, minZFirst float64 = 0, 1e9
	for _, p := range plan.Placements {
		switch p.OrderID {
		case "last-stop":
			if top := p.Z + p.HeightIn; top > maxTopLast {
				maxTopLast = top
			}
		case "first-stop":
			if p.Z < minZFirst {
				minZFirst = p.Z
			}
		}
	}
	if minZFirst < maxTopLast-1e-9 {
		t.Errorf("first stop's material (z≥%.1f) must sit on top of the last stop's tier (tops out %.1f)", minZFirst, maxTopLast)
	}

	// Steps are a 1..N permutation in pack order.
	for i, p := range plan.Placements {
		if p.Step != i+1 {
			t.Fatalf("placement %d has step %d; want %d", i, p.Step, i+1)
		}
	}

	if plan.MaxLoadHeightIn <= 0 {
		t.Errorf("expected a positive max load height, got %v", plan.MaxLoadHeightIn)
	}
	if plan.GVWStatus == "" {
		t.Error("expected a GVW status")
	}
}

func TestSolveSequencedBundlesRespectsBedEnvelope(t *testing.T) {
	v := sequencedTestVehicle()
	stops := []StopItems{
		{OrderID: "o1", StopSequence: 1, Items: []Item{
			{ProductID: "p1", SKU: "2x4", Quantity: 400, LengthIn: 96, WidthIn: 3.5, HeightIn: 1.5, WeightLbs: 9, Stackable: true},
			{ProductID: "p2", SKU: "NO-GEOM", Quantity: 5, WeightLbs: 10},
		}},
	}
	plan := SolveSequencedBundles(v, stops)

	for _, p := range plan.Placements {
		if p.X+p.LengthIn > v.BedLengthIn+1e-9 || p.Y+p.WidthIn > v.BedWidthIn+1e-9 || p.Z+p.HeightIn > v.BedHeightIn+1e-9 {
			t.Fatalf("placement %s exceeds bed envelope: x=%v y=%v z=%v", p.SKU, p.X, p.Y, p.Z)
		}
	}
	if len(plan.Unplaced) == 0 {
		t.Error("expected the no-geometry item to be reported unplaced")
	}
}

func TestBundleShapeSquareish(t *testing.T) {
	it := Item{LengthIn: 96, WidthIn: 3.5, HeightIn: 1.5}
	cols, layers := bundleShape(96, it, sequencedTestVehicle(), 96)
	if cols < 2 || layers < 2 {
		t.Errorf("expected a stacked bundle cross-section, got %d cols × %d layers", cols, layers)
	}
	if float64(layers)*it.HeightIn > 30+1e-9 {
		t.Errorf("bundle %d layers exceeds the 30in band cap", layers)
	}
}

func TestSecurementPlan(t *testing.T) {
	stops := []StopItems{
		{OrderID: "o1", StopSequence: 1, Items: []Item{
			{ProductID: "p1", SKU: "2x6x16", Quantity: 60, LengthIn: 192, WidthIn: 5.5, HeightIn: 1.5, WeightLbs: 27, Stackable: true},
		}},
	}
	plan := SolveSequencedBundles(sequencedTestVehicle(), stops)
	s := plan.Securement
	if s == nil {
		t.Fatal("expected a securement plan")
	}
	// 16 ft article → 2 for first 10 ft + 1 for the fraction = 3 straps.
	if len(s.Straps) < 3 {
		t.Errorf("16 ft load needs ≥3 tie-downs, got %d", len(s.Straps))
	}
	if s.MinAggregateWLLLbs < int64(0.5*60*27) {
		t.Errorf("aggregate WLL %d below 50%% of cargo", s.MinAggregateWLLLbs)
	}
	var sum int64
	for _, st := range s.Straps {
		sum += st.RequiredWLLLbs
		if st.OverHeightIn <= 0 {
			t.Errorf("strap %d has no over-height", st.Number)
		}
	}
	if sum < s.MinAggregateWLLLbs {
		t.Errorf("strap WLL shares (%d) do not cover the aggregate requirement (%d)", sum, s.MinAggregateWLLLbs)
	}
}
