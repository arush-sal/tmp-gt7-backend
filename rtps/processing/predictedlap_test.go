package processing

import (
	"context"
	"math"
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

// TestRefractionOnCenterline_RejectsCrossoverTeleport: a point that projects FAR forward (a
// self-crossing / seam mis-projection, as at Suzuka's figure-8) must not pin the rest of the lap high.
// Without rejection, the greedy monotonic clamp collapses every later sample onto the teleported arc.
func TestRefractionOnCenterline_RejectsCrossoverTeleport(t *testing.T) {
	cl := [][2]float64{{0, 0}, {50, 0}, {100, 0}} // straight centerline, arc 0..100
	arc := geomArc(cl)
	// Lap advances 0,10, then one sample mis-projects to x=90 (arc 0.9), then resumes 20,30.
	mk := func(x float32) Sample { return Sample{PosX: x, PosZ: 0, Distance: float64(x)} }
	n := Normalize([]Sample{mk(0), mk(10), mk(90), mk(20), mk(30)})
	refractionOnCenterline(n, cl, arc, 100)

	got := make([]float64, len(n.Points))
	for i, p := range n.Points {
		got[i] = p.Fraction
	}
	// Monotonic non-decreasing (locate's invariant) and NOT collapsed onto ~0.9.
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("fractions not monotonic: %v", got)
		}
	}
	if got[2] > 0.5 {
		t.Errorf("outlier point fraction = %v, want held near prev (~0.1), not the 0.9 teleport", got[2])
	}
	if last := got[len(got)-1]; last > 0.5 {
		t.Errorf("lap collapsed: last fraction = %v (want ~0.3, real progress preserved)", last)
	}
}

// geomArc is a tiny local alias so the test doesn't import geom directly (it's already a dep of the
// package under test).
func geomArc(cl [][2]float64) []float64 {
	arc := make([]float64, len(cl))
	for i := 1; i < len(cl); i++ {
		dx, dz := cl[i][0]-cl[i-1][0], cl[i][1]-cl[i-1][1]
		arc[i] = arc[i-1] + math.Hypot(dx, dz)
	}
	return arc
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

// clLTrace builds a lap that drives the L-shaped centerline (0,0)->(100,0)->(100,100) exactly, sampled
// every 5 units of arc (0..200), so PosX/PosZ lie on the centerline. distortDist warps the recorded
// cumulative Distance non-linearly (quad growth), so Normalize's per-lap fraction (Distance/total)
// does NOT equal arc/200 — reproducing the two-axis mismatch. bandTimes are the wall-clock durations of
// arc bands [0,80],[80,150],[150,200] (the three mock sectors after re-fractioning), letting each lap
// own a chosen sector.
func clLTrace(session string, lap int32, base time.Time, distort float64, bandTimes [3]time.Duration) []Sample {
	posAt := func(s float64) (float32, float32) {
		if s <= 100 {
			return float32(s), 0
		}
		return 100, float32(s - 100)
	}
	timeAt := func(s float64) time.Duration {
		switch {
		case s <= 80:
			return time.Duration(float64(bandTimes[0]) * (s / 80))
		case s <= 150:
			return bandTimes[0] + time.Duration(float64(bandTimes[1])*((s-80)/70))
		default:
			return bandTimes[0] + bandTimes[1] + time.Duration(float64(bandTimes[2])*((s-150)/50))
		}
	}
	var out []Sample
	for s := 0.0; s <= 200.0001; s += 5 {
		px, pz := posAt(s)
		out = append(out, Sample{
			SessionID: session,
			LapNumber: lap,
			Distance:  s * (1 + distort*s/200), // non-linear -> per-lap fraction != arc fraction
			RecvTime:  base.Add(timeAt(s)),
			PosX:      px,
			PosZ:      pz,
			Speed:     50,
			Gear:      3,
		})
	}
	return out
}

// TestPredictedReferenceLap_CenterlineContinuity: two laps on the SAME L-shaped centerline but with
// different total per-lap distances (different Distance distortion). With the centerline re-fractioning
// fix, a shared sector fraction maps to the same physical point on both laps, so the spliced predicted
// lap has no large PosX/PosZ jump at the sector seams (the pre-fix ~15 m diagonal is gone), and sectors
// still splice in fastest-owner order.
func TestPredictedReferenceLap_CenterlineContinuity(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// Lap 1 owns sectors 0 & 2 (fast there); lap 2 owns sector 1. Different distortion => different totals.
	_ = traces.SaveBatch(clLTrace("s", 1, base, 0.0, [3]time.Duration{30 * time.Second, 40 * time.Second, 30 * time.Second}))
	_ = traces.SaveBatch(clLTrace("s", 2, base, 0.3, [3]time.Duration{35 * time.Second, 30 * time.Second, 35 * time.Second}))

	laps := []Lap{
		{SessionID: "s", LapNumber: 1, Duration: 100 * time.Second, Timed: true, Complete: true},
		{SessionID: "s", LapNumber: 2, Duration: 100 * time.Second, Timed: true, Complete: true},
	}
	// Mock sectors are 0/.4/.75/1 -> arc bands [0,80],[80,150],[150,200] on the 200 m centerline.
	cl := [][2]float64{{0, 0}, {100, 0}, {100, 100}}
	p := fakeProvider{sectors: mockSectors(), reason: "", cl: cl}

	got, err := PredictedReferenceLap(context.Background(), p, traces, "s", laps)
	if err != nil {
		t.Fatalf("PredictedReferenceLap: %v", err)
	}
	wantOwner := []int32{1, 2, 1}
	for i, w := range wantOwner {
		if got.Sectors[i].OwnerLap != w {
			t.Errorf("sector %d OwnerLap = %d, want %d", i, got.Sectors[i].OwnerLap, w)
		}
	}
	// No large position step at any seam. Samples are 5 m apart on the centerline; the only owner change
	// is at a shared boundary, which now lands on the same physical point (step ~0 or one sample gap).
	// Pre-fix, lap 2's fraction-0.4 point sat ~15 m further along its distorted axis -> a >10 m jump.
	const maxStep = 10.0
	for i := 1; i < len(got.Samples); i++ {
		a, b := got.Samples[i-1], got.Samples[i]
		if step := math.Hypot(float64(b.PosX-a.PosX), float64(b.PosZ-a.PosZ)); step > maxStep {
			t.Errorf("position step %.2f m at sample %d exceeds %.0f m (splice discontinuity)", step, i, maxStep)
		}
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
