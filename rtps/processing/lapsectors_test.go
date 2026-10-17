package processing

import (
	"context"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/reference"
)

// fakeProvider is a fixed SectorProvider: returns its sectors + reason regardless of session, and for
// a track slug its byTrack entry when present (else the same fixed sectors + reason).
type fakeProvider struct {
	sectors []reference.Sector
	reason  string
	byTrack map[string][]reference.Sector
}

func (f fakeProvider) SectorsForSession(_ context.Context, _ string) ([]reference.Sector, string, error) {
	return f.sectors, f.reason, nil
}

func (f fakeProvider) SectorsForTrack(_ context.Context, slug string) ([]reference.Sector, string, error) {
	if secs, ok := f.byTrack[slug]; ok {
		return secs, "", nil
	}
	return f.sectors, f.reason, nil
}

// lsTrace builds a lap's trace at the 3-sector mock boundaries (fractions 0/.4/.75/1), timed so each
// sector's duration is that fraction-span of dur. distScale lets a lap end at distance 100 (full) or
// short (to make it non-analyzable in span while keeping a distance axis).
func lsTrace(session string, lap int32, base time.Time, dur time.Duration) []Sample {
	fracs := []float64{0, 0.4, 0.75, 1.0}
	dists := []float64{0, 40, 75, 100}
	out := make([]Sample, len(fracs))
	for i := range fracs {
		out[i] = Sample{
			SessionID: session,
			LapNumber: lap,
			Distance:  dists[i],
			RecvTime:  base.Add(time.Duration(fracs[i] * float64(dur))),
		}
	}
	return out
}

func mockSectors() []reference.Sector {
	_, secs := reference.MockTrack() // 3 sectors 0-.4-.75-1
	return secs
}

// TestSessionLapSectors_OffTrackNotBest: an off-track (all-four-off) sector must not set delta_to_best.
// Lap 1 runs sector 0 faster (20s) but off-track; lap 2 runs it slower (30s) clean. The best for sector
// 0 is lap 2's clean 30s, so lap 2's delta is 0 (it IS the best) and lap 1's dirty sector reports a
// negative delta (faster than the clean best) rather than becoming the best itself.
func TestSessionLapSectors_OffTrackNotBest(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch([]Sample{
		sfSample("s", 1, base, 0, 0, "TTTT"),
		sfSample("s", 1, base, 20, 10*time.Second, "GGGG"), // mid-sector-0 off track
		sfSample("s", 1, base, 40, 20*time.Second, "TTTT"),
		sfSample("s", 1, base, 75, 50*time.Second, "TTTT"),
		sfSample("s", 1, base, 100, 80*time.Second, "TTTT"),
	})
	_ = traces.SaveBatch([]Sample{
		sfSample("s", 2, base, 0, 0, "TTTT"),
		sfSample("s", 2, base, 40, 30*time.Second, "TTTT"),
		sfSample("s", 2, base, 75, 60*time.Second, "TTTT"),
		sfSample("s", 2, base, 100, 95*time.Second, "TTTT"),
	})
	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 80 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 95 * time.Second, Timed: true, Complete: true},
	}
	out, err := SessionLapSectors(context.Background(), fakeProvider{sectors: mockSectors()}, traces, "s", laps)
	if err != nil {
		t.Fatalf("SessionLapSectors: %v", err)
	}
	// out[1] is lap 2 (the clean, slower sector-0 lap) -> it is the sector-0 best -> delta 0.
	s2 := out[1].Splits[0]
	if !s2.HasDelta || s2.DeltaToBest != 0 {
		t.Errorf("lap 2 sector 0: HasDelta=%v DeltaToBest=%v, want true/0 (clean best)", s2.HasDelta, s2.DeltaToBest)
	}
	// out[0] is lap 1 (dirty, faster sector 0) -> delta vs the clean 30s best is 20s-30s = -10s.
	s1 := out[0].Splits[0]
	if s1.DeltaToBest != -10*time.Second {
		t.Errorf("lap 1 sector 0 DeltaToBest = %v, want -10s (dirty fast vs clean best, not the best itself)", s1.DeltaToBest)
	}
}

// TestSessionLapSectors_AllLapsInOrder: every input lap (including one with no samples) appears in
// output, in order, with real-RKB reliability where analyzable.
func TestSessionLapSectors_AllLapsInOrder(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(lsTrace("s", 1, base, 90*time.Second))
	_ = traces.SaveBatch(lsTrace("s", 2, base, 93*time.Second))
	// lap 3: no samples saved.

	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 90 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 93 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 3, Duration: 91 * time.Second, Timed: true, Complete: true},
	}
	p := fakeProvider{sectors: mockSectors(), reason: ""}

	got, err := SessionLapSectors(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("SessionLapSectors: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d lap-sectors, want 3 (never drop a lap)", len(got))
	}
	for i, want := range []int32{1, 2, 3} {
		if got[i].LapNumber != want {
			t.Errorf("position %d LapNumber = %d, want %d (order preserved)", i, got[i].LapNumber, want)
		}
		if len(got[i].Splits) != 3 {
			t.Errorf("lap %d: %d splits, want 3", got[i].LapNumber, len(got[i].Splits))
		}
	}
	// laps 1 & 2 are analyzable -> Reliable; lap 3 has no trace -> not analyzable.
	if !got[0].Reliable || got[0].Reason != "" {
		t.Errorf("lap 1: Reliable=%v Reason=%q, want true/empty", got[0].Reliable, got[0].Reason)
	}
	if got[2].Reliable || got[2].Reason != "trace-span-mismatch" {
		t.Errorf("lap 3 (no trace): Reliable=%v Reason=%q, want false/trace-span-mismatch", got[2].Reliable, got[2].Reason)
	}
	// Split label format.
	if got[0].Splits[0].Label != "S1" || got[0].Splits[2].Label != "S3" {
		t.Errorf("labels = %q..%q, want S1..S3", got[0].Splits[0].Label, got[0].Splits[2].Label)
	}
}

// TestSessionLapSectors_BestPoolExcludesUnanalyzable: a lap with a fast-but-unanalyzable trace must
// not set a bogus best; the best pool is drawn only from analyzable laps.
func TestSessionLapSectors_BestPoolExcludesUnanalyzable(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// lap 1: clean 90s.
	_ = traces.SaveBatch(lsTrace("s", 1, base, 90*time.Second))
	// lap 2: GT7 says 92s, but trace spans only 30s -> fast sectors, but NOT analyzable.
	_ = traces.SaveBatch(lsTrace("s", 2, base, 30*time.Second))

	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 90 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 92 * time.Second, Timed: true, Complete: true},
	}
	p := fakeProvider{sectors: mockSectors(), reason: ""}

	got, err := SessionLapSectors(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("SessionLapSectors: %v", err)
	}
	// lap 1 owns the best for every sector (only analyzable lap) -> its DeltaToBest == 0.
	for _, sp := range got[0].Splits {
		if !sp.HasDelta || sp.DeltaToBest != 0 {
			t.Errorf("lap 1 sector %d: HasDelta=%v DeltaToBest=%v, want true/0 (it owns the best)", sp.Index, sp.HasDelta, sp.DeltaToBest)
		}
	}
	// lap 2's fast sectors are real deltas vs lap 1's best -> negative (they never became the best).
	if got[1].Reliable {
		t.Errorf("lap 2 should be unreliable (span mismatch)")
	}
	sawNeg := false
	for _, sp := range got[1].Splits {
		if sp.HasDelta && sp.DeltaToBest < 0 {
			sawNeg = true
		}
	}
	if !sawNeg {
		t.Errorf("lap 2 (faster but unanalyzable) should show negative DeltaToBest vs the analyzable best")
	}
}

// TestSessionLapSectors_DeltaArithmetic: the lap owning a sector's best has DeltaToBest==0; a slower
// lap has a positive delta.
func TestSessionLapSectors_DeltaArithmetic(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(lsTrace("s", 1, base, 90*time.Second)) // faster everywhere
	_ = traces.SaveBatch(lsTrace("s", 2, base, 96*time.Second)) // slower everywhere

	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 90 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 96 * time.Second, Timed: true, Complete: true},
	}
	p := fakeProvider{sectors: mockSectors(), reason: ""}

	got, err := SessionLapSectors(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("SessionLapSectors: %v", err)
	}
	for _, sp := range got[0].Splits {
		if sp.DeltaToBest != 0 {
			t.Errorf("lap 1 sector %d DeltaToBest = %v, want 0 (owns best)", sp.Index, sp.DeltaToBest)
		}
	}
	for _, sp := range got[1].Splits {
		if !sp.HasDelta || sp.DeltaToBest <= 0 {
			t.Errorf("lap 2 sector %d: HasDelta=%v DeltaToBest=%v, want positive (slower)", sp.Index, sp.HasDelta, sp.DeltaToBest)
		}
	}
}

// TestSessionLapSectors_ProviderReason: provider returns a machine reason (no real geometry) -> all
// laps fall back to 3 mini-sectors, Reliable=false, Reason==the machine reason, deltas still computed.
func TestSessionLapSectors_ProviderReason(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(lsTrace("s", 1, base, 90*time.Second))
	_ = traces.SaveBatch(lsTrace("s", 2, base, 96*time.Second))

	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 90 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 96 * time.Second, Timed: true, Complete: true},
	}
	p := fakeProvider{sectors: nil, reason: "no-centerline"}

	got, err := SessionLapSectors(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("SessionLapSectors: %v", err)
	}
	for _, ls := range got {
		if ls.Reliable {
			t.Errorf("lap %d Reliable=true, want false (provider reason)", ls.LapNumber)
		}
		if ls.Reason != "no-centerline" {
			t.Errorf("lap %d Reason=%q, want no-centerline", ls.LapNumber, ls.Reason)
		}
		if len(ls.Splits) != 3 { // MiniSectors(3) fallback
			t.Errorf("lap %d has %d splits, want 3 mini-sectors", ls.LapNumber, len(ls.Splits))
		}
	}
	// Delta still computed among analyzable laps (both analyzable here): lap 2 slower -> positive.
	for _, sp := range got[1].Splits {
		if !sp.HasDelta || sp.DeltaToBest <= 0 {
			t.Errorf("lap 2 mini-sector %d: HasDelta=%v DeltaToBest=%v, want positive", sp.Index, sp.HasDelta, sp.DeltaToBest)
		}
	}
}

// TestSessionLapSectors_ExcludesNonRacingLaps: the formation/out lap (Timed=false) and the partial
// final lap (Complete=false) cover only part of the track, so they must NOT set the sector best and
// must NOT receive a delta — otherwise the real PB shows a huge bogus delta. Regression for the bug the
// live-replay e2e caught (a 1.9s partial-lap sector became the "best", making the PB read +57s).
func TestSessionLapSectors_ExcludesNonRacingLaps(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(lsTrace("s", 0, base, 5*time.Second))  // formation/out lap: short, partial
	_ = traces.SaveBatch(lsTrace("s", 1, base, 96*time.Second)) // racing, slower
	_ = traces.SaveBatch(lsTrace("s", 2, base, 90*time.Second)) // racing, PB
	_ = traces.SaveBatch(lsTrace("s", 3, base, 4*time.Second))  // partial final lap

	laps := []Lap{
		{SessionID: "s", LapNumber: 0, Duration: 5 * time.Second, Timed: false, Complete: true},
		{SessionID: "s", LapNumber: 1, Duration: 96 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 90 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 3, Duration: 4 * time.Second, Timed: true, Complete: false},
	}
	p := fakeProvider{sectors: mockSectors(), reason: ""}

	got, err := SessionLapSectors(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("SessionLapSectors: %v", err)
	}
	// Formation lap (idx 0) and partial final lap (idx 3): splits served, but no fabricated delta.
	for _, idx := range []int{0, 3} {
		for _, sp := range got[idx].Splits {
			if sp.HasDelta {
				t.Errorf("lap %d sector %d: HasDelta=true, want false (non-racing lap must not get a delta)", got[idx].LapNumber, sp.Index)
			}
		}
	}
	// PB (lap 2) owns every sector best -> DeltaToBest == 0, NOT polluted by the partial laps.
	for _, sp := range got[2].Splits {
		if !sp.HasDelta || sp.DeltaToBest != 0 {
			t.Errorf("PB lap 2 sector %d: HasDelta=%v DeltaToBest=%v, want true/0 (owns the best)", sp.Index, sp.HasDelta, sp.DeltaToBest)
		}
	}
	// Slower racing lap (lap 1) has positive deltas vs the PB best.
	for _, sp := range got[1].Splits {
		if !sp.HasDelta || sp.DeltaToBest <= 0 {
			t.Errorf("lap 1 sector %d: HasDelta=%v DeltaToBest=%v, want positive (slower than PB)", sp.Index, sp.HasDelta, sp.DeltaToBest)
		}
	}
}

// TestSessionLapSectors_NoAnalyzablePool: when no lap is analyzable, HasDelta is false everywhere and
// DeltaToBest is 0 (no pool to compare against).
func TestSessionLapSectors_NoAnalyzablePool(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// single lap, trace span (10s) far from its 90s duration -> not analyzable.
	_ = traces.SaveBatch(lsTrace("s", 1, base, 10*time.Second))

	laps := []Lap{{SessionID: "s", LapNumber: 1, Duration: 90 * time.Second, Timed: true, Complete: true}}
	p := fakeProvider{sectors: mockSectors(), reason: ""}

	got, err := SessionLapSectors(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("SessionLapSectors: %v", err)
	}
	if len(got) != 1 || got[0].Reliable {
		t.Fatalf("want 1 unreliable lap, got %d (reliable=%v)", len(got), got[0].Reliable)
	}
	for _, sp := range got[0].Splits {
		if sp.HasDelta || sp.DeltaToBest != 0 {
			t.Errorf("sector %d: HasDelta=%v DeltaToBest=%v, want false/0 (empty pool)", sp.Index, sp.HasDelta, sp.DeltaToBest)
		}
	}
}

// TestSessionLapSectors_PerLapTrack: a session that drove two tracks splits each lap on its OWN track's
// sectors and pools the per-sector best per track; an unlabelled lap falls back to the session's track.
func TestSessionLapSectors_PerLapTrack(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	for lap := int32(1); lap <= 3; lap++ {
		_ = traces.SaveBatch(lsTrace("s", lap, base.Add(time.Duration(lap)*time.Hour), 100*time.Second))
	}
	half := []reference.Sector{{Index: 0, Start: 0, End: 0.5}, {Index: 1, Start: 0.5, End: 1}}
	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 100 * time.Second, Timed: true, Complete: true, TrackSlug: "a"},
		{SessionID: "s", LapNumber: 2, Duration: 100 * time.Second, Timed: true, Complete: true, TrackSlug: "b"},
		{SessionID: "s", LapNumber: 3, Duration: 100 * time.Second, Timed: true, Complete: true},
	}
	p := fakeProvider{sectors: mockSectors(), byTrack: map[string][]reference.Sector{"a": half, "b": MiniSectors(4)}}
	out, err := SessionLapSectors(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{2, 4, 3} {
		if len(out[i].Splits) != want {
			t.Errorf("lap %d: %d splits, want %d (its own track's sectors)", out[i].LapNumber, len(out[i].Splits), want)
		}
		for _, sp := range out[i].Splits {
			if !sp.HasDelta || sp.DeltaToBest != 0 { // alone on its track: it is that track's best
				t.Errorf("lap %d %s: HasDelta=%v delta=%v, want true/0", out[i].LapNumber, sp.Label, sp.HasDelta, sp.DeltaToBest)
			}
		}
	}
}
