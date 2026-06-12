package load

import (
	"fmt"
	"math"
)

// Working load limits for the strap types we recommend.
const (
	wll2InRatchetLbs = 3335 // 2" ratchet strap
	wll4InWinchLbs   = 5400 // 4" winch strap
)

// computeSecurement derives the tie-down plan for a packed load:
//   - aggregate WLL ≥ 50% of cargo weight (FMCSA §393.106(d) / NSC 10),
//   - two tie-downs for the first 10 ft of load length plus one per
//     additional 10 ft or fraction thereof (§393.110),
//   - straps spaced evenly across the load span, inset from the ends,
//     each annotated with the load height it crosses over.
func computeSecurement(plan *Plan, v Vehicle) {
	if len(plan.Placements) == 0 {
		return
	}

	minX, maxX := math.Inf(1), 0.0
	var cargo float64
	for _, p := range plan.Placements {
		cargo += p.WeightLbs
		if p.X < minX {
			minX = p.X
		}
		if end := p.X + p.LengthIn; end > maxX {
			maxX = end
		}
	}
	span := maxX - minX
	spanFt := span / 12.0

	// Tie-down count by article length.
	n := 2
	if spanFt > 10 {
		n = 2 + int(math.Ceil((spanFt-10)/10))
	}

	aggregate := int64(math.Ceil(cargo * 0.5))

	// Per-strap share; escalate strap count until a 4" winch strap covers it.
	perStrap := int64(math.Ceil(float64(aggregate) / float64(n)))
	for perStrap > wll4InWinchLbs {
		n++
		perStrap = int64(math.Ceil(float64(aggregate) / float64(n)))
	}
	recommended := fmt.Sprintf("2\" ratchet strap (WLL %d lb)", wll2InRatchetLbs)
	if perStrap > wll2InRatchetLbs {
		recommended = fmt.Sprintf("4\" winch strap (WLL %d lb)", wll4InWinchLbs)
	}

	// Even spacing, inset from the load ends.
	inset := math.Min(24, span*0.1)
	first := minX + inset
	last := maxX - inset
	straps := make([]Strap, 0, n)
	for i := 0; i < n; i++ {
		pos := first
		if n > 1 {
			pos = first + (last-first)*float64(i)/float64(n-1)
		} else {
			pos = (first + last) / 2
		}
		straps = append(straps, Strap{
			Number:         i + 1,
			PositionIn:     math.Round(pos*10) / 10,
			OverHeightIn:   loadHeightAt(plan.Placements, pos),
			RequiredWLLLbs: perStrap,
		})
	}

	plan.Securement = &Securement{
		CargoWeightLbs:     int64(math.Round(cargo)),
		MinAggregateWLLLbs: aggregate,
		Straps:             straps,
		RecommendedStrap:   recommended,
		Notes: []string{
			fmt.Sprintf("Aggregate WLL must be ≥ 50%% of cargo weight (%d lb) — FMCSA §393.106 / NSC Std 10.", aggregate),
			"Use edge protectors wherever a strap crosses a board edge.",
			"Tighten winches/ratchets after the first 50 miles and re-check at every stop.",
			"Load is tiered by stop — re-strap the remaining tiers after each delivery.",
		},
	}
}

// loadHeightAt returns the tallest stack the strap crosses at position x.
func loadHeightAt(placements []Placement, x float64) float64 {
	h := 0.0
	for _, p := range placements {
		if p.X <= x && x <= p.X+p.LengthIn {
			if top := p.Z + p.HeightIn; top > h {
				h = top
			}
		}
	}
	return math.Round(h*10) / 10
}
