package tui

import (
	"testing"
	"time"
)

func TestParseSnapshot_Live(t *testing.T) {
	js := []byte(`{"served_at":"2026-07-05T00:00:00Z","status":"live",
      "frame":{"car_speed":214.5,"current_gear":4,"suggested_gear":5,"rpm":7200,
        "rpm_rev_warning":7000,"throttle":1,"brake":0,"current_fuel":14.2,"fuel_capacity":60,
        "tyre_temp_fl":88,"tyre_temp_fr":96,"tyre_temp_rl":82,"tyre_temp_rr":90,
        "current_lap":8,"total_laps":20,"current_position":3,"total_positions":16,
        "best_lap":"117.842s","last_lap":"118.5s","in_race":true},
      "derived":{"current_lap_time_ns":77210000000,"fuel_pct":23.7,"source":"SOURCE_LIVE"},
      "enrichment":{"session_id":"sess-1","analysis":{"session_id":"sess-1","has_pb":true,"pb_lap":2}}}`)
	s, err := ParseSnapshot(js)
	if err != nil {
		t.Fatalf("ParseSnapshot: %v", err)
	}
	if s.Status != "live" || s.Frame == nil {
		t.Fatalf("status=%q frame=%v", s.Status, s.Frame)
	}
	if s.Frame.CarSpeed != 214.5 || s.Frame.CurrentGear != 4 || s.Frame.CurrentLap != 8 {
		t.Errorf("frame fields: %+v", s.Frame)
	}
	if s.Frame.BestLap() != 117842*time.Millisecond {
		t.Errorf("BestLap = %v, want 117.842s", s.Frame.BestLap())
	}
	if s.Derived == nil || s.Derived.CurrentLapTimeNs != 77210000000 {
		t.Errorf("derived: %+v", s.Derived)
	}
	if s.Enrichment.Analysis == nil || !s.Enrichment.Analysis.HasPB {
		t.Errorf("enrichment analysis: %+v", s.Enrichment)
	}
}

func TestParseSnapshot_Idle(t *testing.T) {
	s, err := ParseSnapshot([]byte(`{"status":"idle","frame":null,"derived":null,"enrichment":{"analysis":null}}`))
	if err != nil {
		t.Fatalf("idle: %v", err)
	}
	if s.Status != "idle" || s.Frame != nil || s.Enrichment.Analysis != nil {
		t.Errorf("idle snapshot wrong: %+v", s)
	}
	if s.Frame.BestLap() != 0 { // nil-safe
		t.Errorf("nil frame BestLap should be 0")
	}
}
