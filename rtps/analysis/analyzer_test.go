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
	res, err := a.AnalyzeSession(context.Background(), "s1", 90*s)
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

// TestAnalyzeSession_SectorsOnTheLapsOwnTrack: in a session that drove two tracks, each lap is split
// on its own track, and only laps on the PB lap's track are compared sector by sector.
func TestAnalyzeSession_SectorsOnTheLapsOwnTrack(t *testing.T) {
	laps := processing.NewMemLapStore()
	traces := processing.NewMemTraceStore()
	base := time.Unix(1_700_000_000, 0)
	tr, secs := reference.MockTrack()
	_ = laps.Save(&processing.Lap{SessionID: "m", LapNumber: 1, Duration: 90 * s, Timed: true, Complete: true, Valid: true, TrackSlug: tr.ID})
	_ = laps.Save(&processing.Lap{SessionID: "m", LapNumber: 2, Duration: 93 * s, Timed: true, Complete: true, Valid: true, TrackSlug: "elsewhere"})
	_ = traces.SaveBatch(traceFor("m", 1, base, 90*s))
	_ = traces.SaveBatch(traceFor("m", 2, base, 93*s))
	kb := reference.NewStaticKB()
	kb.Add("other-session", tr, secs) // the session itself resolves to nothing: only the laps' tracks do
	a := Analyzer{Laps: laps, Traces: traces, KB: kb, FallbackSectors: 3}

	res, err := a.AnalyzeSession(context.Background(), "m", 90*s)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range res.Laps {
		switch {
		case l.LapNumber == 1 && len(l.SectorDeltas) != 3:
			t.Errorf("lap 1 (PB track) sector deltas = %d, want 3", len(l.SectorDeltas))
		case l.LapNumber == 2 && len(l.SectorDeltas) != 0:
			t.Errorf("lap 2 (another track) sector deltas = %v, want none", l.SectorDeltas)
		}
	}
	if len(res.BestSectors) != 3 || res.PredictedBest != 90*s {
		t.Errorf("predicted best = %v from %d sectors, want 90s from lap 1's 3", res.PredictedBest, len(res.BestSectors))
	}
}

// TestAnalyzeSession_PartialTraceNotSectorAnalyzed guards the sector-analyzability fix: a lap whose
// trace does not faithfully cover it (a mid-race join lap or a dropout splice — its trace spans far
// less wall-clock than the lap's authoritative GT7 duration) must be kept out of the sector-based
// analysis. Because contiguous mini-sectors telescope (Σ sector times == trace wall-clock span), an
// unguarded partial lap contributes tiny sector times that win every "best sector" and collapse the
// predicted best to nonsense (observed live: a 94s PB reporting a 16.88s predicted best). The lap
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

	res, err := a.AnalyzeSession(context.Background(), "p", 90*s)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}

	// PB is duration-based, so the partial lap does not change it: lap 1 @ 90s.
	if res.PBLap != 1 || res.PBTime != 90*s {
		t.Fatalf("PB = lap %d @ %v, want lap 1 @ 90s", res.PBLap, res.PBTime)
	}
	// Predicted best must stay plausible: bounded by the PB and NOT collapsed to the partial lap.
	if res.PredictedBest > res.PBTime {
		t.Errorf("predicted best %v > PB %v", res.PredictedBest, res.PBTime)
	}
	if res.PredictedBest < res.PBTime/2 {
		t.Errorf("predicted best %v implausibly small vs PB %v — partial-trace lap leaked in", res.PredictedBest, res.PBTime)
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

// traceForFracs is traceFor with caller-supplied distance fractions, so a test can shape *where*
// within the lap the sector boundaries land in wall-clock time (traceFor always uses the mock
// track's own 0/.4/.75/1 boundaries, which isn't enough to make one lap win a sector over another
// when both laps have the same total Duration).
func traceForFracs(session string, lap int32, base time.Time, dur time.Duration, fracs []float64) []processing.Sample {
	dists := []float64{0, 40, 75, 100} // matches MockTrack's sector boundaries in % of lap distance
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

// TestAnalyzeSession_FallbackLapExcludedFromMatch guards matchLap's explicit l.Fallback exclusion.
// A fallback lap's Duration is a wall-clock span (GT7 last_lap was unavailable), not an authoritative
// time, so it can coincidentally land within matchLap's 2ms tolerance of gt7Best. Here the fallback
// lap is the *only* lap within tolerance — the genuine lap sits 50ms away — so an unguarded matchLap
// would return the fallback lap's number. With the guard, matchLap must skip it and report no match.
func TestAnalyzeSession_FallbackLapExcludedFromMatch(t *testing.T) {
	laps := processing.NewMemLapStore()
	traces := processing.NewMemTraceStore()
	base := time.Unix(1_700_000_000, 0)

	// lap 1: genuine GT7-timed lap, but 50ms off gt7Best — outside matchLap's 2ms tolerance, so it
	//        can never be the match itself. Its only role is to prove the fallback lap isn't picked.
	// lap 2: a Fallback lap whose wall-clock Duration sits inside matchLap's 2ms tolerance of
	//        gt7Best — the only candidate matchLap would pick if the Fallback guard were removed.
	_ = laps.Save(&processing.Lap{SessionID: "f", LapNumber: 1, Duration: 90*s + 50*time.Millisecond, Timed: true, Complete: true, Valid: true})
	_ = laps.Save(&processing.Lap{SessionID: "f", LapNumber: 2, Duration: 90*s + 1*time.Millisecond, Timed: true, Complete: true, Valid: true, Fallback: true})
	_ = traces.SaveBatch(traceFor("f", 1, base, 90*s+50*time.Millisecond))
	_ = traces.SaveBatch(traceFor("f", 2, base, 90*s+1*time.Millisecond))

	kb := reference.NewStaticKB()
	tr, secs := reference.MockTrack()
	kb.Add("f", tr, secs)
	a := Analyzer{Laps: laps, Traces: traces, KB: kb, FallbackSectors: 3}

	res, err := a.AnalyzeSession(context.Background(), "f", 90*s)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if res.PBLap == 2 {
		t.Fatalf("PBLap = %d, the fallback lap must never be selected as the matched lap", res.PBLap)
	}
	if res.PBLap != 0 {
		t.Errorf("PBLap = %d, want 0 (no non-fallback lap is within tolerance of gt7Best)", res.PBLap)
	}
}

// TestAnalyzeSession_FallbackLapExcludedFromSectors guards the sector loop's explicit l.Fallback
// exclusion. Both laps here share the same total Duration (so both trivially pass sectorAnalyzable),
// but the fallback lap's trace places its sector-1 boundary earlier in wall-clock time, making its
// sector 1 genuinely faster than the genuine lap's sector 1. Absent the guard, the fallback lap would
// win that BestSectors slot; with it, only the genuine lap may.
func TestAnalyzeSession_FallbackLapExcludedFromSectors(t *testing.T) {
	laps := processing.NewMemLapStore()
	traces := processing.NewMemTraceStore()
	base := time.Unix(1_700_000_000, 0)

	// lap 1: genuine, even sector split (sector 1 = 0.4 of 90s = 36s).
	// lap 2: fallback, same 90s total but sector 1 lands at 0.2 of the lap (18s) — strictly faster
	//        than lap 1's sector 1, so it WOULD win that BestSectors slot if the guard were removed.
	_ = laps.Save(&processing.Lap{SessionID: "h", LapNumber: 1, Duration: 90 * s, Timed: true, Complete: true, Valid: true})
	_ = laps.Save(&processing.Lap{SessionID: "h", LapNumber: 2, Duration: 90 * s, Timed: true, Complete: true, Valid: true, Fallback: true})
	_ = traces.SaveBatch(traceFor("h", 1, base, 90*s))
	_ = traces.SaveBatch(traceForFracs("h", 2, base, 90*s, []float64{0, 0.2, 0.75, 1.0}))

	kb := reference.NewStaticKB()
	tr, secs := reference.MockTrack()
	kb.Add("h", tr, secs)
	a := Analyzer{Laps: laps, Traces: traces, KB: kb, FallbackSectors: 3}

	res, err := a.AnalyzeSession(context.Background(), "h", 90*s)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	for _, b := range res.BestSectors {
		if b.LapNumber == 2 {
			t.Errorf("fallback lap 2 supplied best sector %d — Fallback laps must not feed sector analysis", b.Index)
		}
	}
	// Positive check (so this test can't pass vacuously on an empty BestSectors): sector 0 spans
	// distance fraction 0-0.4, exactly the segment the fallback lap made artificially fast (its
	// second sample lands at distance 40 but wall-clock 0.2*90s = 18s vs the genuine lap's 36s).
	// With the guard in place, the genuine lap 1 is the only sector-0 candidate and must own it.
	var sawSector0 bool
	for _, b := range res.BestSectors {
		if b.Index == 0 {
			sawSector0 = true
			if b.LapNumber != 1 {
				t.Errorf("sector 0 best owned by lap %d, want lap 1 (the genuine lap)", b.LapNumber)
			}
		}
	}
	if !sawSector0 {
		t.Fatal("BestSectors has no entry for sector 0 — expected the genuine lap to win it")
	}
}

// TestAnalyzeSession_FallbackLapExcludedFromLapsList guards the res.Laps loop's explicit l.Fallback
// exclusion (analyzer.go's second lap loop, distinct from the sector loop and matchLap). A fallback
// lap's Duration is a wall-clock span, not an authoritative GT7 time, so it can still be Timed &&
// Complete and land within the gt7Best*3/2 band that gates res.Laps — nothing else would keep it out.
// Here the fallback lap sits well inside that band alongside a genuine lap; absent the guard it would
// appear in res.Laps too.
func TestAnalyzeSession_FallbackLapExcludedFromLapsList(t *testing.T) {
	laps := processing.NewMemLapStore()
	traces := processing.NewMemTraceStore()
	base := time.Unix(1_700_000_000, 0)

	// lap 1: genuine, matches gt7Best exactly (also serves as PB).
	// lap 2: Fallback, 100s — comfortably inside the 135s (gt7Best*3/2) band that gates res.Laps, so
	//        it WOULD be listed if the l.Fallback guard in the res.Laps loop were removed.
	_ = laps.Save(&processing.Lap{SessionID: "j", LapNumber: 1, Duration: 90 * s, Timed: true, Complete: true, Valid: true})
	_ = laps.Save(&processing.Lap{SessionID: "j", LapNumber: 2, Duration: 100 * s, Timed: true, Complete: true, Valid: true, Fallback: true})
	_ = traces.SaveBatch(traceFor("j", 1, base, 90*s))

	kb := reference.NewStaticKB()
	tr, secs := reference.MockTrack()
	kb.Add("j", tr, secs)
	a := Analyzer{Laps: laps, Traces: traces, KB: kb, FallbackSectors: 3}

	res, err := a.AnalyzeSession(context.Background(), "j", 90*s)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}

	var sawGenuine bool
	for _, l := range res.Laps {
		if l.LapNumber == 2 {
			t.Fatalf("fallback lap 2 appears in res.Laps — Fallback laps must not be listed")
		}
		if l.LapNumber == 1 {
			sawGenuine = true
		}
	}
	if !sawGenuine {
		t.Fatal("genuine lap 1 missing from res.Laps")
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

	res, err := a.AnalyzeSession(context.Background(), "q", 0)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if res.HasPB || len(res.Laps) != 0 || len(res.Recommendations) != 1 {
		t.Errorf("no-valid-lap: hasPB=%v laps=%d recs=%d, want false/0/1", res.HasPB, len(res.Laps), len(res.Recommendations))
	}
}

// TestAnalyzeSession_TrustsGT7Best: when the GT7-reported best is faster than any captured lap (the
// fastest lap was missed by the trace/lap store), PB is still the GT7 value, PBLap is 0 (no captured
// lap matches it), and each lap's delta is computed relative to the GT7 best, not a captured lap.
func TestAnalyzeSession_TrustsGT7Best(t *testing.T) {
	laps := processing.NewMemLapStore()
	_ = laps.Save(&processing.Lap{SessionID: "g", LapNumber: 2, Duration: 80090 * time.Millisecond, Timed: true, Complete: true})
	a := Analyzer{Laps: laps, Traces: processing.NewMemTraceStore(), KB: reference.NewStaticKB(), FallbackSectors: 3}
	res, err := a.AnalyzeSession(context.Background(), "g", 79858*time.Millisecond) // GT7 best faster than lap 2
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasPB || res.PBTime != 79858*time.Millisecond {
		t.Fatalf("PBTime = %v (hasPB=%v), want 79.858s from GT7 best", res.PBTime, res.HasPB)
	}
	if res.PBLap != 0 {
		t.Errorf("PBLap = %d, want 0 (no captured lap matches GT7 best)", res.PBLap)
	}
	for _, l := range res.Laps {
		if l.LapNumber == 2 && l.DeltaToPB != 80090*time.Millisecond-79858*time.Millisecond {
			t.Errorf("lap 2 delta = %v, want relative to GT7 best", l.DeltaToPB)
		}
	}
}

// TestAnalyzeSession_PredictedBestSuppressedWithoutPBLap guards BUG-4: PredictedBest is built from
// perLap, a pool the PB lap may not belong to (unmatched here, as in TestAnalyzeSession_TrustsGT7Best
// above — PBLap == 0). Without a guard, the sector-best sum can legitimately exceed the untracked PB
// lap's real sector split, publishing an impossible "prediction" faster than the PB it's supposed to
// bound. When the PB lap isn't in the analyzable pool, PredictedBest/BestSectors/TimeOnTable must be
// suppressed to zero/nil rather than published as an out-of-bounds number.
func TestAnalyzeSession_PredictedBestSuppressedWithoutPBLap(t *testing.T) {
	laps := processing.NewMemLapStore()
	traces := processing.NewMemTraceStore()
	base := time.Unix(1_700_000_000, 0)

	// lap 1 is a captured, sector-analyzable lap, but its duration (95s) does not match gt7Best (90s)
	// within matchLap's tolerance, so PBLap stays 0 — the PB lap's own sectors are never in perLap.
	_ = laps.Save(&processing.Lap{SessionID: "k", LapNumber: 1, Duration: 95 * s, Timed: true, Complete: true, Valid: true})
	_ = traces.SaveBatch(traceFor("k", 1, base, 95*s))

	kb := reference.NewStaticKB()
	tr, secs := reference.MockTrack()
	kb.Add("k", tr, secs)
	a := Analyzer{Laps: laps, Traces: traces, KB: kb, FallbackSectors: 3}

	res, err := a.AnalyzeSession(context.Background(), "k", 90*s)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if res.PBLap != 0 {
		t.Fatalf("PBLap = %d, want 0 (no captured lap matches gt7Best) — test setup invalid", res.PBLap)
	}
	if res.PredictedBest != 0 || res.BestSectors != nil || res.TimeOnTable != 0 {
		t.Errorf("PredictedBest/BestSectors/TimeOnTable = %v/%v/%v, want 0/nil/0 (suppressed: PB lap not in the analyzable pool)",
			res.PredictedBest, res.BestSectors, res.TimeOnTable)
	}
}

// TestAnalyzeSession_PredictedBestBoundedByPB guards the positive side of BUG-4's invariant: when the
// PB lap IS in the analyzable pool (TestAnalyzeSession above matches lap 1 == PB), PredictedBest must
// be published, strictly positive, and never exceed PBTime.
func TestAnalyzeSession_PredictedBestBoundedByPB(t *testing.T) {
	a := newFixtureAnalyzer(t)
	res, err := a.AnalyzeSession(context.Background(), "s1", 90*s)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}
	if res.PBLap == 0 {
		t.Fatal("PBLap = 0, want a matched PB lap — test setup invalid")
	}
	if res.PredictedBest <= 0 {
		t.Errorf("PredictedBest = %v, want > 0 (PB lap is analyzable)", res.PredictedBest)
	}
	if res.PredictedBest > res.PBTime {
		t.Errorf("PredictedBest %v > PBTime %v, want <= PB", res.PredictedBest, res.PBTime)
	}
}
