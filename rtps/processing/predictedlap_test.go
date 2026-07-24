package processing

import (
	"context"
	"testing"
	"time"
)

// plTrace builds a lap's trace at the 3-sector mock boundaries (fractions 0/.4/.75/1) with per-boundary
// wall-clock offsets, so each sector's duration can be set independently (unlike lsTrace which scales
// every sector by one dur). PosX = Distance and PosZ = 0, so the re-accumulated hypot distance tracks
// the distance axis monotonically. offsets has 4 entries (one per boundary); the lap span is offsets[3].
func plTrace(session string, lap int32, base time.Time, offsets []time.Duration, gear int32) []Sample {
	dists := []float64{0, 40, 75, 100}
	out := make([]Sample, len(offsets))
	for i := range offsets {
		out[i] = Sample{
			SessionID: session,
			LapNumber: lap,
			Distance:  dists[i],
			RecvTime:  base.Add(offsets[i]),
			PosX:      float32(dists[i]),
			PosZ:      0,
			Speed:     float32(50 + i),
			Gear:      gear,
		}
	}
	return out
}

// sfSample builds a trace sample at a distance (PosX=dist, PosZ=0 so re-accumulated distance tracks the
// axis), elapsed offset, and per-wheel surface code.
func sfSample(session string, lap int32, base time.Time, dist float64, off time.Duration, surf string) Sample {
	return Sample{
		SessionID: session, LapNumber: lap, Distance: dist, RecvTime: base.Add(off),
		PosX: float32(dist), PosZ: 0, Speed: 100, SurfaceType: surf,
	}
}

// TestSectorStartIndex_NearestToPrev: the seam-alignment start picker. First sector (no prev) starts at
// the first sample in [start,end]; a later sector starts at the sample nearest the previous owner's last
// position within [start-seamSearchMargin, end], so two owners meet at ~the same physical point.
func TestSectorStartIndex_NearestToPrev(t *testing.T) {
	var pts []NormSample // x=0..100 step 5, Fraction = x/100, index = x/5
	for x := 0; x <= 100; x += 5 {
		pts = append(pts, NormSample{Sample: Sample{PosX: float32(x), PosZ: 0}, Fraction: float64(x) / 100})
	}
	// First sector [0.4,0.6], no prev -> first sample at/after 0.4 = x40 = idx 8.
	if got := sectorStartIndex(pts, 0.4, 0.6, false, 0, 0); got != 8 {
		t.Errorf("first-sector start = %d, want 8 (frac 0.4)", got)
	}
	// Seam: prev owner ended near x=52; sector [0.5,0.7] window [0.45,0.7] -> nearest is x50 = idx 10.
	if got := sectorStartIndex(pts, 0.5, 0.7, true, 52, 0); got != 10 {
		t.Errorf("seam start = %d, want 10 (x50 nearest prev x52)", got)
	}
	// Prev slightly BEFORE the boundary (x=46) -> the margin sample x45 (frac 0.45 < start) is nearest,
	// so the owner starts just before its distance-fraction Start to stay spatially continuous. idx 9.
	if got := sectorStartIndex(pts, 0.5, 0.7, true, 46, 0); got != 9 {
		t.Errorf("seam start (margin) = %d, want 9 (x45 nearest prev x46)", got)
	}
}

// TestPredictedReferenceLap_CoversFullLapNoGap: the splice cuts each owner's sector by its own per-lap
// distance fraction and emits raw samples, so the predicted lap covers the whole lap with no dropped
// interior stretch (the regression this fix targets: centerline projection used to drop crossover
// samples, leaving a bare band segment). A dense single-lap pool owns every sector; assert the spliced
// fractions span 0..1 with no interior gap larger than the sample spacing.
func TestPredictedReferenceLap_CoversFullLapNoGap(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// 40 evenly-spaced samples over distance 0..100 (fractions 0..1 at 1/39 spacing), 100s lap.
	var samples []Sample
	for i := 0; i < 40; i++ {
		samples = append(samples, sfSample("s", 1, base, float64(i)*100.0/39.0, time.Duration(i)*100*time.Second/39, "TTTT"))
	}
	_ = traces.SaveBatch(samples)
	laps := []Lap{{SessionID: "s", LapNumber: 1, Duration: 100 * time.Second, Timed: true, Complete: true}}

	got, err := PredictedReferenceLap(context.Background(), fakeProvider{sectors: mockSectors()}, traces, "s", laps)
	if err != nil {
		t.Fatalf("PredictedReferenceLap: %v", err)
	}
	if len(got.Samples) == 0 {
		t.Fatal("no predicted samples")
	}
	if got.Samples[0].Fraction != 0 || got.Samples[len(got.Samples)-1].Fraction < 0.999 {
		t.Errorf("span = [%.3f, %.3f], want ~[0,1]", got.Samples[0].Fraction, got.Samples[len(got.Samples)-1].Fraction)
	}
	const maxGap = 0.05 // sample spacing is 1/39 ~= 0.026; a dropped stretch would exceed this
	for i := 1; i < len(got.Samples); i++ {
		if step := got.Samples[i].Fraction - got.Samples[i-1].Fraction; step > maxGap {
			t.Errorf("interior fraction gap %.3f at sample %d exceeds %.3f (a stretch was dropped)", step, i, maxGap)
		}
	}
}

// TestPredictedReferenceLap_ExcludesOffTrackSector: lap 1 is fastest in sector 0 (20s) but runs it
// off-track (all four tyres on grass mid-sector); lap 2 runs sector 0 slower (30s) but clean. The
// off-track sector must NOT own — sector 0's owner is lap 2.
func TestPredictedReferenceLap_ExcludesOffTrackSector(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// Sectors: mock 0/.4/.75/1. Distances /100 = fractions. Lap 1 sector0=20s with a grass sample at .2.
	_ = traces.SaveBatch([]Sample{
		sfSample("s", 1, base, 0, 0, "TTTT"),
		sfSample("s", 1, base, 20, 10*time.Second, "GGGG"), // mid-sector-0, all four off track
		sfSample("s", 1, base, 40, 20*time.Second, "TTTT"), // sector0 boundary -> dur 20s
		sfSample("s", 1, base, 75, 50*time.Second, "TTTT"),
		sfSample("s", 1, base, 100, 80*time.Second, "TTTT"),
	})
	// Lap 2: sector0=30s, clean throughout.
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
	got, err := PredictedReferenceLap(context.Background(), fakeProvider{sectors: mockSectors()}, traces, "s", laps)
	if err != nil {
		t.Fatalf("PredictedReferenceLap: %v", err)
	}
	if got.Sectors[0].OwnerLap != 2 {
		t.Errorf("sector 0 OwnerLap = %d, want 2 (lap 1's faster sector 0 was off-track)", got.Sectors[0].OwnerLap)
	}
}

// TestPredictedReferenceLap_AllOffTrackFallback: when EVERY lap runs a sector off-track, the predicted
// lap is kept whole — the sector falls back to the min-duration (dirty) owner rather than being dropped.
func TestPredictedReferenceLap_AllOffTrackFallback(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// Both laps run sector 0 off-track; lap 1 is faster there (20s vs 30s).
	_ = traces.SaveBatch([]Sample{
		sfSample("s", 1, base, 0, 0, "TTTT"),
		sfSample("s", 1, base, 20, 10*time.Second, "GGGG"),
		sfSample("s", 1, base, 40, 20*time.Second, "TTTT"),
		sfSample("s", 1, base, 75, 50*time.Second, "TTTT"),
		sfSample("s", 1, base, 100, 80*time.Second, "TTTT"),
	})
	_ = traces.SaveBatch([]Sample{
		sfSample("s", 2, base, 0, 0, "TTTT"),
		sfSample("s", 2, base, 20, 15*time.Second, "GGGG"),
		sfSample("s", 2, base, 40, 30*time.Second, "TTTT"),
		sfSample("s", 2, base, 75, 60*time.Second, "TTTT"),
		sfSample("s", 2, base, 100, 95*time.Second, "TTTT"),
	})
	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 80 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 95 * time.Second, Timed: true, Complete: true},
	}
	got, err := PredictedReferenceLap(context.Background(), fakeProvider{sectors: mockSectors()}, traces, "s", laps)
	if err != nil {
		t.Fatalf("PredictedReferenceLap: %v", err)
	}
	if len(got.Sectors) != 3 {
		t.Fatalf("Sectors = %d, want 3 (predicted lap kept whole despite all-dirty sector)", len(got.Sectors))
	}
	if got.Sectors[0].OwnerLap != 1 {
		t.Errorf("sector 0 OwnerLap = %d, want 1 (fastest dirty as fallback)", got.Sectors[0].OwnerLap)
	}
}

// TestPredictedReferenceLap_ExcludesUndrivenSector: a lap whose trace does not cover a sector produces
// a 0-duration split that is vacuously "clean" (no samples in range). Such a split must NOT own the
// sector — owner is the lap that actually drove it. Guards the regression where a 0-duration clean
// split beat the real fastest lap.
func TestPredictedReferenceLap_ExcludesUndrivenSector(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// Lap 1 covers the whole lap (fractions 0..1), sector0 = 30s, clean.
	_ = traces.SaveBatch([]Sample{
		sfSample("s", 1, base, 0, 0, "TTTT"),
		sfSample("s", 1, base, 40, 30*time.Second, "TTTT"),
		sfSample("s", 1, base, 75, 60*time.Second, "TTTT"),
		sfSample("s", 1, base, 100, 90*time.Second, "TTTT"),
	})
	// Lap 2's samples only span the second half (fractions .6..1) -> sector0 [0,0.4] has no samples ->
	// 0-duration, vacuously clean.
	_ = traces.SaveBatch([]Sample{
		sfSample("s", 2, base, 60, 0, "TTTT"),
		sfSample("s", 2, base, 75, 20*time.Second, "TTTT"),
		sfSample("s", 2, base, 100, 55*time.Second, "TTTT"),
	})
	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 90 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 55 * time.Second, Timed: true, Complete: true},
	}
	got, err := PredictedReferenceLap(context.Background(), fakeProvider{sectors: mockSectors()}, traces, "s", laps)
	if err != nil {
		t.Fatalf("PredictedReferenceLap: %v", err)
	}
	if got.Sectors[0].OwnerLap != 1 {
		t.Errorf("sector 0 OwnerLap = %d, want 1 (lap 2 never drove sector 0 -> 0-duration must not own)", got.Sectors[0].OwnerLap)
	}
	if got.Sectors[0].DurationNs <= 0 {
		t.Errorf("sector 0 DurationNs = %d, want > 0 (a real driven sector)", got.Sectors[0].DurationNs)
	}
}

// TestPredictedReferenceLap_SplicesFastestSectors: lap 1 is fastest in sector 0 & 2, lap 2 in sector 1.
// The predicted lap must pick the right owner per sector, its predicted best must be the sum of the
// owners' sector durations (below either single lap), and its samples must span fraction 0->1 with
// non-decreasing t_ns and distance.
func TestPredictedReferenceLap_SplicesFastestSectors(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// Lap 1: sector0=30s, sector1=40s, sector2=30s (total 100s).
	_ = traces.SaveBatch(plTrace("s", 1, base, []time.Duration{0, 30 * time.Second, 70 * time.Second, 100 * time.Second}, 3))
	// Lap 2: sector0=35s, sector1=30s, sector2=35s (total 100s).
	_ = traces.SaveBatch(plTrace("s", 2, base, []time.Duration{0, 35 * time.Second, 65 * time.Second, 100 * time.Second}, 4))

	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 100 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 100 * time.Second, Timed: true, Complete: true},
	}
	p := fakeProvider{sectors: mockSectors(), reason: ""}

	got, err := PredictedReferenceLap(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("PredictedReferenceLap: %v", err)
	}
	if !got.Reliable || got.Reason != "" {
		t.Errorf("Reliable=%v Reason=%q, want true/empty", got.Reliable, got.Reason)
	}
	if len(got.Sectors) != 3 {
		t.Fatalf("Sectors = %d, want 3", len(got.Sectors))
	}
	wantOwner := []int32{1, 2, 1}
	for i, w := range wantOwner {
		if got.Sectors[i].OwnerLap != w {
			t.Errorf("sector %d OwnerLap = %d, want %d", i, got.Sectors[i].OwnerLap, w)
		}
	}
	// predicted best = 30 (lap1 s0) + 30 (lap2 s1) + 30 (lap1 s2) = 90s, below either 100s lap.
	if got.PredictedBestNs != int64(90*time.Second) {
		t.Errorf("PredictedBestNs = %d, want %d", got.PredictedBestNs, int64(90*time.Second))
	}
	if got.PredictedBestNs > int64(100*time.Second) {
		t.Errorf("PredictedBestNs %d should not exceed a single lap (100s)", got.PredictedBestNs)
	}
	if len(got.Samples) == 0 {
		t.Fatal("Samples empty, want spliced samples")
	}
	// fraction spans 0->~1, monotonic non-decreasing; t_ns and distance non-decreasing.
	first, last := got.Samples[0], got.Samples[len(got.Samples)-1]
	if first.Fraction != 0 {
		t.Errorf("first fraction = %v, want 0", first.Fraction)
	}
	if last.Fraction < 0.999 {
		t.Errorf("last fraction = %v, want ~1", last.Fraction)
	}
	for i := 1; i < len(got.Samples); i++ {
		if got.Samples[i].Fraction < got.Samples[i-1].Fraction {
			t.Errorf("fraction not monotonic at %d: %v < %v", i, got.Samples[i].Fraction, got.Samples[i-1].Fraction)
		}
		if got.Samples[i].TNs < got.Samples[i-1].TNs {
			t.Errorf("t_ns not monotonic at %d: %d < %d", i, got.Samples[i].TNs, got.Samples[i-1].TNs)
		}
		if got.Samples[i].Distance < got.Samples[i-1].Distance {
			t.Errorf("distance not monotonic at %d: %v < %v", i, got.Samples[i].Distance, got.Samples[i-1].Distance)
		}
	}
	if last.TNs != got.PredictedBestNs {
		t.Errorf("last t_ns = %d, want %d (== predicted best)", last.TNs, got.PredictedBestNs)
	}
}

// TestPredictedReferenceLap_ProviderReason: provider reports no real geometry -> mini-sector fallback,
// Reliable=false, Reason carried through, but a predicted lap is still spliced.
func TestPredictedReferenceLap_ProviderReason(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(plTrace("s", 1, base, []time.Duration{0, 30 * time.Second, 70 * time.Second, 100 * time.Second}, 3))

	laps := []Lap{{SessionID: "s", LapNumber: 1, Duration: 100 * time.Second, Timed: true, Complete: true}}
	p := fakeProvider{sectors: nil, reason: "no-centerline"}

	got, err := PredictedReferenceLap(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("PredictedReferenceLap: %v", err)
	}
	if got.Reliable {
		t.Errorf("Reliable=true, want false (provider reason)")
	}
	if got.Reason != "no-centerline" {
		t.Errorf("Reason=%q, want no-centerline", got.Reason)
	}
	if len(got.Samples) == 0 {
		t.Error("Samples empty, want spliced samples over mini-sectors")
	}
}

// TestPredictedReferenceLap_EmptyPool: no analyzable racing laps -> empty samples, zero predicted best,
// no error.
func TestPredictedReferenceLap_EmptyPool(t *testing.T) {
	traces := NewMemTraceStore()
	p := fakeProvider{sectors: mockSectors(), reason: ""}

	got, err := PredictedReferenceLap(context.Background(), p, traces, "s", nil)
	if err != nil {
		t.Fatalf("PredictedReferenceLap: %v", err)
	}
	if len(got.Samples) != 0 {
		t.Errorf("Samples = %d, want 0 (empty pool)", len(got.Samples))
	}
	if got.PredictedBestNs != 0 {
		t.Errorf("PredictedBestNs = %d, want 0", got.PredictedBestNs)
	}
}
