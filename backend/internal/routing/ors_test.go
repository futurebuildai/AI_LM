package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sampleStops() []Stop {
	return []Stop{
		{OrderID: "a", Lat: 49.10, Lng: -119.10, WeightLbs: 1000},
		{OrderID: "b", Lat: 49.30, Lng: -119.40, WeightLbs: 1000},
		{OrderID: "c", Lat: 49.05, Lng: -119.05, WeightLbs: 1000},
	}
}

func orderIDs(stops []Stop) []string {
	out := make([]string, len(stops))
	for i, s := range stops {
		out[i] = s.OrderID
	}
	return out
}

// TestORSProviderFallbackNoKey verifies that with no API key the provider
// degrades to the exact haversine optimization (never hard-fails).
func TestORSProviderFallbackNoKey(t *testing.T) {
	depotLat, depotLng := 49.0, -119.0
	p := NewORSProvider("", "", "")
	if p.Configured() {
		t.Fatal("provider with empty key should report not configured")
	}

	got, gotDist, gotDur := p.Sequence(context.Background(), depotLat, depotLng, sampleStops())
	want, wantDist, wantDur := OptimizeSequence(depotLat, depotLng, sampleStops())

	if a, b := orderIDs(got), orderIDs(want); !equalStrings(a, b) {
		t.Fatalf("fallback order mismatch: got %v want %v", a, b)
	}
	if gotDist != wantDist || gotDur != wantDur {
		t.Fatalf("fallback totals mismatch: got (%.2f,%.2f) want (%.2f,%.2f)", gotDist, gotDur, wantDist, wantDur)
	}
}

// TestORSProviderFallbackOnError verifies that an ORS error (non-200 here) is
// swallowed and the haversine optimizer is used instead.
func TestORSProviderFallbackOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"quota exceeded"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	depotLat, depotLng := 49.0, -119.0
	p := NewORSProvider("test-key", srv.URL, "driving-hgv")
	got, _, _ := p.Sequence(context.Background(), depotLat, depotLng, sampleStops())
	want, _, _ := OptimizeSequence(depotLat, depotLng, sampleStops())

	if a, b := orderIDs(got), orderIDs(want); !equalStrings(a, b) {
		t.Fatalf("error fallback order mismatch: got %v want %v", a, b)
	}
}

// TestORSProviderMatrixSequencing verifies a happy-path matrix call: the
// returned order follows the road matrix (not haversine), the request uses
// [lng,lat] ordering + the driving-hgv profile + raw-key Authorization, and the
// totals come from the matrix (distance in mi, duration sec→min).
func TestORSProviderMatrixSequencing(t *testing.T) {
	depotLat, depotLng := 49.0, -119.0

	// Matrix index 0 = depot; stop k = index k+1. Distances (mi).
	dist := [][]float64{
		{0, 10, 20, 1}, // depot
		{10, 0, 2, 5},  // stop a (idx1)
		{20, 2, 0, 8},  // stop b (idx2)
		{1, 5, 8, 0},   // stop c (idx3)
	}
	dur := make([][]float64, 4)
	for i := range dist {
		dur[i] = make([]float64, 4)
		for j := range dist[i] {
			dur[i][j] = dist[i][j] * 60 // seconds
		}
	}

	var gotProfile, gotAuth string
	var gotLocations [][2]float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProfile = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body orsMatrixRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotLocations = body.Locations
		_ = json.NewEncoder(w).Encode(orsMatrixResponse{Distances: dist, Durations: dur})
	}))
	defer srv.Close()

	p := NewORSProvider("secret-key", srv.URL, "driving-hgv")
	got, gotDist, gotDur := p.Sequence(context.Background(), depotLat, depotLng, sampleStops())

	// NN from depot: c(1) → a(5) → b(2): expected order c, a, b.
	if want := []string{"c", "a", "b"}; !equalStrings(orderIDs(got), want) {
		t.Fatalf("matrix order mismatch: got %v want %v", orderIDs(got), want)
	}
	for i, s := range got {
		if s.Sequence != i+1 {
			t.Fatalf("sequence not contiguous: stop %d has sequence %d", i, s.Sequence)
		}
	}
	// distance = 1 + 5 + 2 = 8 mi; duration = 480 sec / 60 = 8 min.
	if gotDist != 8 || gotDur != 8 {
		t.Fatalf("matrix totals mismatch: got (%.2f mi, %.2f min) want (8, 8)", gotDist, gotDur)
	}

	if gotProfile != "/v2/matrix/driving-hgv" {
		t.Errorf("unexpected matrix path: %s", gotProfile)
	}
	if gotAuth != "secret-key" {
		t.Errorf("ORS POST should send raw key in Authorization, got %q", gotAuth)
	}
	if len(gotLocations) != 4 {
		t.Fatalf("expected 4 locations (depot + 3 stops), got %d", len(gotLocations))
	}
	// [lng,lat] ordering: depot location must be [depotLng, depotLat].
	if gotLocations[0][0] != depotLng || gotLocations[0][1] != depotLat {
		t.Errorf("depot coord not [lng,lat]: got %v want [%v,%v]", gotLocations[0], depotLng, depotLat)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
