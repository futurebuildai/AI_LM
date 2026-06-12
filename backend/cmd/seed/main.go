// Command seed populates AI_LM with demo fleet profiles and restricted points.
// Vehicle ids are stable demo UUIDs; in a real deployment they would match
// GableLBM vehicle ids pulled via the integration API.
package main

import (
	"context"
	"log"
	"time"

	"github.com/futurebuildai/ai-lm/internal/compliance"
	"github.com/futurebuildai/ai-lm/internal/config"
	"github.com/futurebuildai/ai-lm/internal/fleet"
	"github.com/futurebuildai/ai-lm/pkg/database"
)

func i64(v int64) *int64       { return &v }
func f64(v float64) *float64   { return &v }

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	db, err := database.Connect(cfg.DatabaseURL, database.DefaultPoolConfig())
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fleetSvc := fleet.NewService(fleet.NewRepository(db))
	complianceSvc := compliance.NewService(compliance.NewRepository(db))

	// --- Demo fleet profiles (Kelowna LBM dealer) ---
	profiles := []struct {
		vehicleID string
		input     fleet.ProfileInput
	}{
		{
			vehicleID: "11111111-1111-1111-1111-111111111111",
			input: fleet.ProfileInput{
				Name:          "Freightliner M2 Flatbed",
				BedLengthIn:   288, // 24 ft
				BedWidthIn:    96,  // 8 ft
				BedHeightIn:   96,
				GVWRLbs:       33000,
				TareWeightLbs: 14000,
				Axles: []fleet.AxleInput{
					{AxleNumber: 1, MaxWeightLbs: 12000, PositionFromFrontIn: 0, AxleType: "STEER"},
					{AxleNumber: 2, MaxWeightLbs: 21000, PositionFromFrontIn: 240, AxleType: "DRIVE"},
				},
			},
		},
		{
			vehicleID: "22222222-2222-2222-2222-222222222222",
			input: fleet.ProfileInput{
				Name:          "International Box Truck",
				BedLengthIn:   312, // 26 ft
				BedWidthIn:    100,
				BedHeightIn:   102,
				GVWRLbs:       26000,
				TareWeightLbs: 12500,
				Axles: []fleet.AxleInput{
					{AxleNumber: 1, MaxWeightLbs: 10000, PositionFromFrontIn: 0, AxleType: "STEER"},
					{AxleNumber: 2, MaxWeightLbs: 17500, PositionFromFrontIn: 260, AxleType: "DRIVE"},
				},
			},
		},
	}
	for _, p := range profiles {
		if _, err := fleetSvc.UpsertProfile(ctx, p.vehicleID, p.input); err != nil {
			log.Fatalf("seed vehicle profile %s: %v", p.input.Name, err)
		}
		log.Printf("seeded vehicle profile: %s", p.input.Name)
	}

	// --- Demo restricted points (Okanagan corridor) ---
	points := []compliance.RestrictedPointInput{
		{
			Name:              "Bennett Bridge (W.R. Bennett)",
			Lat:               49.8845,
			Lng:               -119.4960,
			RestrictionType:   "WEIGHT",
			MaxGrossWeightLbs: i64(21000), // demo-calibrated: a loaded flatbed trips this
			Notes:             "Floating bridge — temporary gross-weight restriction during deck repair.",
		},
		{
			Name:            "Highway 97 CN Overpass",
			Lat:             49.8612,
			Lng:             -119.4490,
			RestrictionType: "HEIGHT",
			MaxHeightIn:     f64(136), // 11'4" — demo-calibrated: tall lumber tiers trip this
			Notes:           "Low clearance overpass.",
		},
		{
			Name:             "McCulloch Rd Culvert",
			Lat:              49.8420,
			Lng:              -119.3700,
			RestrictionType:  "WEIGHT",
			MaxAxleWeightLbs: i64(18000),
			Notes:            "Seasonal axle-weight limit on rural culvert.",
		},
	}
	for _, pt := range points {
		if _, err := complianceSvc.CreatePoint(ctx, pt); err != nil {
			log.Fatalf("seed restricted point %s: %v", pt.Name, err)
		}
		log.Printf("seeded restricted point: %s", pt.Name)
	}

	log.Println("seed complete")
}
