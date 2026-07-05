package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/arush-sal/garage/backend/rtps/analysis"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func TestRenderLive_ShowsDirectMetrics(t *testing.T) {
	s := LiveSnapshot{
		Status: "live",
		Frame: &LiveFrame{
			CarSpeed: 214, CurrentGear: 4, SuggestedGear: 5, Rpm: 7200,
			Throttle: 1, Brake: 0, CurrentLap: 8, TotalLaps: 20,
			CurrentPosition: 3, TotalPositions: 16, InRace: true,
			TyreTempFL: 88, TyreTempFR: 96, TyreTempRL: 82, TyreTempRR: 90,
			CurrentFuel: 14.2, FuelCapacity: 60,
		},
		Derived: &LiveDerived{CurrentLapTimeNs: 77210000000, FuelPct: 23.7},
	}
	out := stripANSI(RenderLive(s, 120))
	for _, want := range []string{"214", "GEAR", "P3/16", "LAP", "8/20"} {
		if !strings.Contains(out, want) {
			t.Errorf("live render missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "GEAR   4") {
		t.Errorf("expected gear line to show 4:\n%s", out)
	}
	// enrichment absent -> ANALYSIS panel shows its placeholder, not a crash
	if !strings.Contains(out, "no analysis yet") {
		t.Errorf("expected analysis placeholder for absent enrichment:\n%s", out)
	}
}

func TestRenderLive_Idle(t *testing.T) {
	out := stripANSI(RenderLive(LiveSnapshot{Status: "idle"}, 120))
	if !strings.Contains(strings.ToLower(out), "waiting") && !strings.Contains(strings.ToLower(out), "idle") {
		t.Errorf("idle render should indicate no telemetry:\n%s", out)
	}
}

func TestRenderLive_WithEnrichment(t *testing.T) {
	s := LiveSnapshot{
		Status: "live", Frame: &LiveFrame{CarSpeed: 100, CurrentLap: 3},
		Derived: &LiveDerived{},
		Enrichment: LiveEnrichment{SessionID: "sess-1", Analysis: &analysis.SessionAnalysis{
			HasPB: true, PBLap: 2, Recommendations: []string{"Personal Best: lap 2 at 1:57.842."},
		}},
	}
	out := stripANSI(RenderLive(s, 120))
	if !strings.Contains(out, "Personal Best") {
		t.Errorf("expected PB recommendation surfaced:\n%s", out)
	}
}
