package load

import (
	"math"
	"sort"
)

// Solver computes a load plan for a vehicle and a set of items. Implementations
// must be deterministic so the same input always yields the same plan.
type Solver interface {
	Solve(v Vehicle, items []Item) Plan
}

// thresholds for axle/GVW utilization status.
const (
	warnUtilization = 0.90
	failUtilization = 1.00
)

// ShelfSolver is the MVP deterministic placement heuristic. It lays unit boxes
// in rows across the bed width, advancing along the bed length, stacking a
// second layer for stackable items when height allows. Weight is distributed to
// axles by longitudinal position using linear interpolation between the two
// bracketing axles.
type ShelfSolver struct{}

// NewShelfSolver returns the default deterministic solver.
func NewShelfSolver() *ShelfSolver { return &ShelfSolver{} }

// unit is a single expanded box (one quantity of an item).
type unit struct {
	item  Item
	index int
}

func (s *ShelfSolver) Solve(v Vehicle, items []Item) Plan {
	plan := Plan{
		GableVehicleID: v.GableVehicleID,
		Placements:     []Placement{},
		AxleLoads:      []AxleLoad{},
		Unplaced:       []string{},
	}

	// Expand items into individual unit boxes.
	var units []unit
	for _, it := range items {
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		for i := 0; i < qty; i++ {
			units = append(units, unit{item: it})
		}
	}

	// Heaviest first improves stability (heavy on the bottom layer) and yields a
	// deterministic order. Tie-break on SKU for determinism.
	sort.SliceStable(units, func(i, j int) bool {
		if units[i].item.WeightLbs != units[j].item.WeightLbs {
			return units[i].item.WeightLbs > units[j].item.WeightLbs
		}
		return units[i].item.SKU < units[j].item.SKU
	})

	// Shelf packing cursor.
	var cursorX float64 // current row start along length
	var cursorY float64 // current position across width
	var rowDepth float64 // max length of items in current row
	var rowHeight float64 // max height in current row (for stacking ceiling)

	for _, u := range units {
		it := u.item
		if it.LengthIn <= 0 || it.WidthIn <= 0 || it.HeightIn <= 0 {
			plan.Unplaced = append(plan.Unplaced, it.SKU)
			continue
		}

		// Wrap to a new row when the item exceeds remaining width.
		if cursorY+it.WidthIn > v.BedWidthIn {
			cursorX += rowDepth
			cursorY = 0
			rowDepth = 0
			rowHeight = 0
		}

		// Out of bed length → cannot place.
		if cursorX+it.LengthIn > v.BedLengthIn {
			plan.Unplaced = append(plan.Unplaced, it.SKU)
			continue
		}

		z := 0.0
		// Stack a second layer if stackable and there is headroom in this row slot.
		if it.Stackable && rowHeight > 0 && rowHeight+it.HeightIn <= v.BedHeightIn {
			z = rowHeight
		}

		p := Placement{
			ItemID:    it.ProductID,
			SKU:       it.SKU,
			X:         cursorX,
			Y:         cursorY,
			Z:         z,
			LengthIn:  it.LengthIn,
			WidthIn:   it.WidthIn,
			HeightIn:  it.HeightIn,
			WeightLbs: it.WeightLbs,
			AxleGroup: nearestAxle(v.Axles, cursorX+it.LengthIn/2),
		}
		plan.Placements = append(plan.Placements, p)

		cursorY += it.WidthIn
		if it.LengthIn > rowDepth {
			rowDepth = it.LengthIn
		}
		if z == 0 && it.HeightIn > rowHeight {
			rowHeight = it.HeightIn
		}
	}

	computeAxleLoads(&plan, v)
	return plan
}

// computeAxleLoads distributes cargo + tare weight across axles, sets each
// axle's status, the overall GVW status, total weight and balance score.
func computeAxleLoads(plan *Plan, v Vehicle) {
	axles := v.Axles
	loads := make([]float64, len(axles))

	// Distribute tare proportional to each axle's rated capacity (heavier-rated
	// axles carry more of the chassis). Falls back to even split.
	var totalRating int64
	for _, a := range axles {
		totalRating += a.MaxWeightLbs
	}
	for i, a := range axles {
		if totalRating > 0 {
			loads[i] += float64(v.TareWeightLbs) * float64(a.MaxWeightLbs) / float64(totalRating)
		} else if len(axles) > 0 {
			loads[i] += float64(v.TareWeightLbs) / float64(len(axles))
		}
	}

	// Distribute each placement's weight to its bracketing axles by position.
	var cargo float64
	for _, p := range plan.Placements {
		cargo += p.WeightLbs
		distributeToAxles(loads, axles, p.X+p.LengthIn/2, p.WeightLbs)
	}

	plan.TotalWeightLbs = int64(math.Round(cargo)) + v.TareWeightLbs

	worst := statusRank("PASS")
	utils := make([]float64, 0, len(axles))
	for i, a := range axles {
		w := int64(math.Round(loads[i]))
		util := 0.0
		if a.MaxWeightLbs > 0 {
			util = loads[i] / float64(a.MaxWeightLbs)
		}
		st := utilStatus(util)
		if statusRank(st) > worst {
			worst = statusRank(st)
		}
		utils = append(utils, util)
		plan.AxleLoads = append(plan.AxleLoads, AxleLoad{
			AxleNumber:   a.AxleNumber,
			WeightLbs:    w,
			MaxWeightLbs: a.MaxWeightLbs,
			Utilization:  round3(util),
			Status:       st,
		})
	}

	// Overall GVW: compare total to GVWR as well.
	if v.GVWRLbs > 0 {
		gvwUtil := float64(plan.TotalWeightLbs) / float64(v.GVWRLbs)
		if statusRank(utilStatus(gvwUtil)) > worst {
			worst = statusRank(utilStatus(gvwUtil))
		}
	}
	plan.GVWStatus = rankStatus(worst)
	plan.BalanceScore = round3(balanceScore(utils))
}

// distributeToAxles allocates weight at longitudinal position x to the two
// axles that bracket x (linear interpolation). Weight outside the axle span is
// assigned fully to the nearest end axle.
func distributeToAxles(loads []float64, axles []Axle, x, weight float64) {
	n := len(axles)
	if n == 0 {
		return
	}
	if n == 1 {
		loads[0] += weight
		return
	}
	// Before the first axle.
	if x <= axles[0].PositionFromFrontIn {
		loads[0] += weight
		return
	}
	// After the last axle.
	if x >= axles[n-1].PositionFromFrontIn {
		loads[n-1] += weight
		return
	}
	for i := 0; i < n-1; i++ {
		a, b := axles[i], axles[i+1]
		if x >= a.PositionFromFrontIn && x <= b.PositionFromFrontIn {
			span := b.PositionFromFrontIn - a.PositionFromFrontIn
			if span <= 0 {
				loads[i] += weight
				return
			}
			frac := (x - a.PositionFromFrontIn) / span
			loads[i] += weight * (1 - frac)
			loads[i+1] += weight * frac
			return
		}
	}
}

func nearestAxle(axles []Axle, x float64) int {
	if len(axles) == 0 {
		return 0
	}
	best := axles[0]
	bestDist := math.Abs(x - axles[0].PositionFromFrontIn)
	for _, a := range axles[1:] {
		d := math.Abs(x - a.PositionFromFrontIn)
		if d < bestDist {
			bestDist = d
			best = a
		}
	}
	return best.AxleNumber
}

func utilStatus(util float64) string {
	switch {
	case util > failUtilization:
		return "FAIL"
	case util >= warnUtilization:
		return "WARN"
	default:
		return "PASS"
	}
}

func statusRank(s string) int {
	switch s {
	case "FAIL":
		return 2
	case "WARN":
		return 1
	default:
		return 0
	}
}

func rankStatus(r int) string {
	switch r {
	case 2:
		return "FAIL"
	case 1:
		return "WARN"
	default:
		return "PASS"
	}
}

// balanceScore returns 1 minus the normalized spread of axle utilizations.
// A perfectly even load scores 1.0; large imbalance trends toward 0.
func balanceScore(utils []float64) float64 {
	if len(utils) == 0 {
		return 0
	}
	var sum float64
	for _, u := range utils {
		sum += u
	}
	mean := sum / float64(len(utils))
	if mean == 0 {
		return 1
	}
	var variance float64
	for _, u := range utils {
		d := u - mean
		variance += d * d
	}
	variance /= float64(len(utils))
	cv := math.Sqrt(variance) / mean // coefficient of variation
	score := 1 - cv
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

func round3(f float64) float64 {
	return math.Round(f*1000) / 1000
}
