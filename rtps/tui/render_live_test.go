package tui

import (
	"errors"
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
	out := stripANSI(RenderLive(s, 120, nil))
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
	out := stripANSI(RenderLive(LiveSnapshot{Status: "idle"}, 120, nil))
	if !strings.Contains(strings.ToLower(out), "waiting") && !strings.Contains(strings.ToLower(out), "idle") {
		t.Errorf("idle render should indicate no telemetry:\n%s", out)
	}
}

// TestRenderLive_IdleWithConnError guards against a permanent stream failure (backend down, 404)
// looking identical to a normal idle "no live session yet" state — a real connection error must
// surface distinctly, and must not also print the plain idle text.
func TestRenderLive_IdleWithConnError(t *testing.T) {
	connErr := errors.New("GET http://localhost:8080/live: 404 Not Found")
	out := stripANSI(RenderLive(LiveSnapshot{Status: "idle"}, 120, connErr))
	if !strings.Contains(out, connErr.Error()) {
		t.Errorf("idle+connErr render should surface the error text:\n%s", out)
	}
	if strings.Contains(out, "Waiting for telemetry") {
		t.Errorf("idle+connErr render should not show the plain idle text:\n%s", out)
	}
}

// TestRenderLive_LiveWithConnError guards against the "permanent failure looks healthy" bug on the
// live path: once a live frame has arrived, m.latest.Status stays "live" even after the stream
// drops (a later stream error only sets the model's lastErr, it does not revert the snapshot to
// idle). RenderLive must still show the last-known telemetry (useful — no need to blank the
// dashboard) but must flag it as stale via the footer, not silently render the plain "streaming"
// footer as if the connection were healthy.
func TestRenderLive_LiveWithConnError(t *testing.T) {
	connErr := errors.New("GET http://localhost:8080/live: 404 Not Found")
	s := LiveSnapshot{
		Status: "live",
		Frame: &LiveFrame{
			CarSpeed: 214, CurrentGear: 4, Rpm: 7200, CurrentLap: 8, TotalLaps: 20,
		},
	}
	out := stripANSI(RenderLive(s, 120, connErr))
	if !strings.Contains(out, "reconnecting") && !strings.Contains(out, "stream lost") {
		t.Errorf("live+connErr render should surface a reconnecting/stale banner:\n%s", out)
	}
	if !strings.Contains(out, "214") {
		t.Errorf("live+connErr render should still show the last-known telemetry (speed):\n%s", out)
	}
	if strings.Contains(out, "streaming · s: sessions · q: quit") {
		t.Errorf("live+connErr render should not show the plain healthy streaming footer:\n%s", out)
	}
}

// TestRenderLive_PedalBarsNormalizeToPercent guards against the pedal bars
// treating Throttle/Brake (0-100 scale) as if they were already 0..1 — a
// bug that made any pedal input above 1% render as a full bar.
func TestRenderLive_PedalBarsNormalizeToPercent(t *testing.T) {
	half := LiveSnapshot{
		Status: "live",
		Frame:  &LiveFrame{CarSpeed: 100, Throttle: 50, Brake: 0},
	}
	out := stripANSI(RenderLive(half, 120, nil))
	lines := strings.Split(out, "\n")
	var throtLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "THROT") {
			throtLine = l
			break
		}
	}
	if throtLine == "" {
		t.Fatalf("no THROT line found:\n%s", out)
	}
	fill := strings.Count(throtLine, "#")
	if fill == 20 || fill == 0 {
		t.Errorf("Throttle:50 should be a roughly half-filled bar, got %d/20 fill chars: %q", fill, throtLine)
	}
	if fill < 8 || fill > 12 {
		t.Errorf("Throttle:50 expected ~10/20 fill chars, got %d: %q", fill, throtLine)
	}

	full := LiveSnapshot{
		Status: "live",
		Frame:  &LiveFrame{CarSpeed: 100, Throttle: 100, Brake: 0},
	}
	outFull := stripANSI(RenderLive(full, 120, nil))
	for _, l := range strings.Split(outFull, "\n") {
		if strings.HasPrefix(l, "THROT") {
			if strings.Count(l, "#") != 20 {
				t.Errorf("Throttle:100 should be a full bar, got %q", l)
			}
			break
		}
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
	out := stripANSI(RenderLive(s, 120, nil))
	if !strings.Contains(out, "Personal Best") {
		t.Errorf("expected PB recommendation surfaced:\n%s", out)
	}
}

// TestRenderLive_ResolvedNoPB guards against conflating "enrichment not
// resolved yet" (Analysis == nil) with "resolved, but no valid lap" (Analysis
// != nil, HasPB == false) — the latter must surface the analyzer's own
// recommendation instead of falling back to the "no analysis yet" placeholder.
func TestRenderLive_ResolvedNoPB(t *testing.T) {
	s := LiveSnapshot{
		Status: "live", Frame: &LiveFrame{CarSpeed: 100, CurrentLap: 1},
		Derived: &LiveDerived{},
		Enrichment: LiveEnrichment{SessionID: "sess-1", Analysis: &analysis.SessionAnalysis{
			HasPB: false,
			Recommendations: []string{
				"No valid (complete, timed, gap-free) lap in this session — no Personal Best to analyze.",
			},
		}},
	}
	out := stripANSI(RenderLive(s, 120, nil))
	if !strings.Contains(out, "No valid") {
		t.Errorf("expected resolved no-PB recommendation surfaced:\n%s", out)
	}
	if strings.Contains(out, "no analysis yet") {
		t.Errorf("resolved no-PB analysis should not show the pending placeholder:\n%s", out)
	}
}

func TestRenderLive_SuggestedGearSentinelHidden(t *testing.T) {
	live := stripANSI(RenderLive(LiveSnapshot{Status: "live", Frame: &LiveFrame{CurrentGear: 3, SuggestedGear: 15}}, 120, nil))
	if strings.Contains(live, "shift") {
		t.Errorf("suggested_gear 15 (no-suggestion sentinel) must not render a shift hint:\n%s", live)
	}
	real := stripANSI(RenderLive(LiveSnapshot{Status: "live", Frame: &LiveFrame{CurrentGear: 3, SuggestedGear: 5}}, 120, nil))
	if !strings.Contains(real, "shift 5") {
		t.Errorf("a genuine suggested gear should render a shift hint:\n%s", real)
	}
}
