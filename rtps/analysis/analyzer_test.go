package analysis

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
)

// traceFor builds a lap's trace with samples at the mock track's sector boundaries (fractions
// 0/.4/.75/1), timed so each sector's duration is that fraction-span of the lap duration.
func traceFor(session string, lap int32, base time.Time, dur time.Duration) []processing.Sample {
	fracs := []float64{0, 0.4, 0.75, 1.0}
	dists := []float64{0, 40, 75, 100}
	out := make([]processing.Sample, len(fracs))
	for i := range fracs {
		out[i] = processing.Sample{
			SessionID: session,
			LapNumber: lap,
			Distance:  dists[i],
			RecvTime:  base.Add(time.Duration(fracs[i] * float64(dur))),
		}
	}
	return out
}

func newFixtureAnalyzer(t *testing.T) Analyzer {
	t.Helper()
	laps := processing.NewMemLapStore()
	traces := processing.NewMemTraceStore()
	base := time.Unix(1_700_000_000, 0)

	// Session s1: formation lap 0 (invalid), lap 1 = PB (90s), lap 2 (93s, slower everywhere).
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 0, Duration: 20 * s, Timed: false, Complete: true, Valid: false})
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, Duration: 90 * s, Timed: true, Complete: true, Valid: true})
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 2, Duration: 93 * s, Timed: true, Complete: true, Valid: true})
	_ = traces.SaveBatch(traceFor("s1", 1, base, 90*s))
	_ = traces.SaveBatch(traceFor("s1", 2, base, 93*s))

	kb := reference.NewStaticKB()
	tr, secs := reference.MockTrack() // 3 sectors at 0-.4-.75-1
	kb.Add("s1", tr, secs)

	return Analyzer{Laps: laps, Traces: traces, KB: kb, FallbackSectors: 3}
}

// TestAnalyzeSession: PB selection, per-lap + per-sector deltas, recommendations.
func TestAnalyzeSession(t *testing.T) {
	a := newFixtureAnalyzer(t)
	res, err := a.AnalyzeSession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if !res.HasPB || res.PBLap != 1 || res.PBTime != 90*s {
		t.Fatalf("PB = lap %d @ %v (hasPB=%v), want lap 1 @ 90s", res.PBLap, res.PBTime, res.HasPB)
	}
	if len(res.Laps) != 2 { // only the two valid laps
		t.Fatalf("analyzed %d laps, want 2", len(res.Laps))
	}

	// Locate lap 2's analysis; delta to PB is 3s, split ~ across the 3 sectors summing to 3s.
	var lap2 LapAnalysis
	for _, l := range res.Laps {
		if l.LapNumber == 2 {
			lap2 = l
		}
	}
	if lap2.DeltaToPB != 3*s {
		t.Errorf("lap2 DeltaToPB = %v, want 3s", lap2.DeltaToPB)
	}
	if len(lap2.SectorDeltas) != 3 {
		t.Fatalf("lap2 sector deltas = %d, want 3", len(lap2.SectorDeltas))
	}
	var sum time.Duration
	for _, sd := range lap2.SectorDeltas {
		if sd.DeltaToPB <= 0 {
			t.Errorf("sector %d delta = %v, want > 0 (slower everywhere)", sd.Index, sd.DeltaToPB)
		}
		sum += sd.DeltaToPB
	}
	if math.Abs((sum - 3*s).Seconds()) > 0.01 {
		t.Errorf("sector deltas sum = %v, want ~3s", sum)
	}
	if len(res.Recommendations) < 2 {
		t.Errorf("recommendations = %v, want >= 2", res.Recommendations)
	}
}

// TestAnalyzeSession_PartialTraceNotSectorAnalyzed guards the sector-analyzability fix: a lap whose
// trace does not faithfully cover it (a mid-race join lap or a dropout splice — its trace spans far
// less wall-clock than the lap's authoritative GT7 duration) must be kept out of the sector-based
// analysis. Because contiguous mini-sectors telescope (Σ sector times == trace wall-clock span), an
// unguarded partial lap contributes tiny sector times that win every "best sector" and collapse the
// theoretical best to nonsense (observed live: a 94s PB reporting a 16.88s theoretical best). The lap
// must still count for PB/consistency (duration-based) but carry no sector deltas.
func TestAnalyzeSession_PartialTraceNotSectorAnalyzed(t *testing.T) {
	laps := processing.NewMemLapStore()
	traces := processing.NewMemTraceStore()
	base := time.Unix(1_700_000_000, 0)

	// lap 1: clean full-trace PB (90s, trace spans 90s).
	// lap 2: clean, slower everywhere (95s, trace spans 95s).
	// lap 3: GT7 calls it a 92s lap, but its trace covers only 15s (join/dropout splice). Unguarded,
	//        its ~15s of sectors would undercut both clean laps and wreck the theoretical best.
	_ = laps.Save(&processing.Lap{SessionID: "p", LapNumber: 1, Duration: 90 * s, Timed: true, Complete: true, Valid: true})
	_ = laps.Save(&processing.Lap{SessionID: "p", LapNumber: 2, Duration: 95 * s, Timed: true, Complete: true, Valid: true})
	_ = laps.Save(&processing.Lap{SessionID: "p", LapNumber: 3, Duration: 92 * s, Timed: true, Complete: true, Valid: true})
	_ = traces.SaveBatch(traceFor("p", 1, base, 90*s))
	_ = traces.SaveBatch(traceFor("p", 2, base, 95*s))
	_ = traces.SaveBatch(traceFor("p", 3, base, 15*s)) // trace span 15s != 92s duration -> not analyzable

	kb := reference.NewStaticKB()
	tr, secs := reference.MockTrack()
	kb.Add("p", tr, secs)
	a := Analyzer{Laps: laps, Traces: traces, KB: kb, FallbackSectors: 3}

	res, err := a.AnalyzeSession(context.Background(), "p")
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}

	// PB is duration-based, so the partial lap does not change it: lap 1 @ 90s.
	if res.PBLap != 1 || res.PBTime != 90*s {
		t.Fatalf("PB = lap %d @ %v, want lap 1 @ 90s", res.PBLap, res.PBTime)
	}
	// Theoretical best must stay plausible: bounded by the PB and NOT collapsed to the partial lap.
	if res.TheoreticalBest > res.PBTime {
		t.Errorf("theoretical best %v > PB %v", res.TheoreticalBest, res.PBTime)
	}
	if res.TheoreticalBest < res.PBTime/2 {
		t.Errorf("theoretical best %v implausibly small vs PB %v — partial-trace lap leaked in", res.TheoreticalBest, res.PBTime)
	}
	// The partial lap must own no best sector.
	for _, b := range res.BestSectors {
		if b.LapNumber == 3 {
			t.Errorf("partial-trace lap 3 supplied best sector %d", b.Index)
		}
	}
	// Core invariant: any lap that carries sector deltas has them reconcile with its own duration.
	for _, l := range res.Laps {
		if len(l.SectorDeltas) == 0 {
			continue // untrusted trace -> sectors omitted
		}
		var sum time.Duration
		for _, sd := range l.SectorDeltas {
			sum += sd.LapTime
		}
		if d := absDur(sum - l.Duration); d > spanTol(l.Duration) {
			t.Errorf("lap %d sector-time sum %v != duration %v (off by %v)", l.LapNumber, sum, l.Duration, d)
		}
	}
	// lap 3 is still reported (duration/delta valid) but with no sector deltas.
	var lap3 *LapAnalysis
	for i := range res.Laps {
		if res.Laps[i].LapNumber == 3 {
			lap3 = &res.Laps[i]
		}
	}
	if lap3 == nil {
		t.Fatal("lap 3 missing from laps[] — it is a valid lap and must still be listed")
	}
	if len(lap3.SectorDeltas) != 0 {
		t.Errorf("lap 3 (untrusted trace) has %d sector deltas, want 0", len(lap3.SectorDeltas))
	}
}

// absDur is |d|; spanTol mirrors the analyzer's sector-analyzability tolerance for test assertions.
func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func spanTol(dur time.Duration) time.Duration {
	tol := 300 * time.Millisecond
	if p := time.Duration(0.005 * float64(dur)); p > tol {
		tol = p
	}
	return tol
}

// TestAnalyzeSession_NoValidLap: an abandoned-style session (no valid lap) has no PB and says so.
func TestAnalyzeSession_NoValidLap(t *testing.T) {
	laps := processing.NewMemLapStore()
	_ = laps.Save(&processing.Lap{SessionID: "q", LapNumber: 0, Duration: 20 * s, Valid: false})
	_ = laps.Save(&processing.Lap{SessionID: "q", LapNumber: 1, Duration: 60 * s, Valid: false})
	a := Analyzer{Laps: laps, Traces: processing.NewMemTraceStore(), KB: reference.NewStaticKB(), FallbackSectors: 3}

	res, err := a.AnalyzeSession(context.Background(), "q")
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if res.HasPB || len(res.Laps) != 0 || len(res.Recommendations) != 1 {
		t.Errorf("no-valid-lap: hasPB=%v laps=%d recs=%d, want false/0/1", res.HasPB, len(res.Laps), len(res.Recommendations))
	}
}
