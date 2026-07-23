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
