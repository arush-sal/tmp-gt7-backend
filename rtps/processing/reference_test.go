package processing

import (
	"testing"
	"time"
)

// refTrace builds a lap's trace at the 3-sector mock boundaries (fractions 0/.4/.75/1) with a
// constant speed, timed so the wall-clock span equals dur (needed for SectorAnalyzable to admit it).
// distEnd sets the final cumulative distance (>0 for a usable distance axis; short-span laps set a
// mismatched dur to make them non-analyzable).
func refTrace(session string, lap int32, base time.Time, dur time.Duration, speed float32) []Sample {
	fracs := []float64{0, 0.4, 0.75, 1.0}
	dists := []float64{0, 40, 75, 100}
	out := make([]Sample, len(fracs))
	for i := range fracs {
		out[i] = Sample{
			SessionID: session,
			LapNumber: lap,
			Distance:  dists[i],
			RecvTime:  base.Add(time.Duration(fracs[i] * float64(dur))),
			Speed:     speed,
		}
	}
	return out
}

func racingLap(session string, lap int32, dur time.Duration) Lap {
	return Lap{SessionID: session, LapNumber: lap, Duration: dur, Timed: true, Complete: true}
}

// allEqual asserts every value in vals equals want (within a tiny epsilon).
func allEqual(t *testing.T, name string, vals []float64, want float64) {
	t.Helper()
	for i, v := range vals {
		if diff := v - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s[%d] = %v, want %v", name, i, v, want)
		}
	}
}

// TestMedianReference_OddCount: 3 laps with constant speed 100/110/120 -> median 110 at every grid
// point.
func TestMedianReference_OddCount(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(refTrace("s", 1, base, 90*time.Second, 100))
	_ = traces.SaveBatch(refTrace("s", 2, base, 90*time.Second, 110))
	_ = traces.SaveBatch(refTrace("s", 3, base, 90*time.Second, 120))

	laps := []Lap{racingLap("s", 1, 90*time.Second), racingLap("s", 2, 90*time.Second), racingLap("s", 3, 90*time.Second)}
	res, err := MedianReference(traces, "s", laps, []string{"speed"}, 10)
	if err != nil {
		t.Fatalf("MedianReference: %v", err)
	}
	if res.LapCount != 3 {
		t.Fatalf("LapCount = %d, want 3", res.LapCount)
	}
	if len(res.Fractions) != 10 {
		t.Fatalf("Fractions len = %d, want 10", len(res.Fractions))
	}
	if res.Fractions[0] != 0 || res.Fractions[9] != 1 {
		t.Errorf("grid endpoints = %v..%v, want 0..1", res.Fractions[0], res.Fractions[9])
	}
	allEqual(t, "speed", res.Series["speed"], 110)
}

// TestMedianReference_EvenCount: 2 laps 100/120 -> average of the two middles = 110.
func TestMedianReference_EvenCount(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(refTrace("s", 1, base, 90*time.Second, 100))
	_ = traces.SaveBatch(refTrace("s", 2, base, 90*time.Second, 120))

	laps := []Lap{racingLap("s", 1, 90*time.Second), racingLap("s", 2, 90*time.Second)}
	res, err := MedianReference(traces, "s", laps, []string{"speed"}, 5)
	if err != nil {
		t.Fatalf("MedianReference: %v", err)
	}
	if res.LapCount != 2 {
		t.Fatalf("LapCount = %d, want 2", res.LapCount)
	}
	allEqual(t, "speed", res.Series["speed"], 110)
}

// TestMedianReference_PoolGate: a formation lap (Timed=false) and a partial lap (Complete=false) with
// wildly different speed must NOT skew the median, and must not count toward LapCount.
func TestMedianReference_PoolGate(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	// Two clean racing laps at 100/120 -> median 110.
	_ = traces.SaveBatch(refTrace("s", 1, base, 90*time.Second, 100))
	_ = traces.SaveBatch(refTrace("s", 2, base, 90*time.Second, 120))
	// Formation lap (lap 0, Timed=false) and a partial lap (Complete=false) with a huge speed.
	_ = traces.SaveBatch(refTrace("s", 0, base, 90*time.Second, 9000))
	_ = traces.SaveBatch(refTrace("s", 3, base, 90*time.Second, 9000))

	laps := []Lap{
		{SessionID: "s", LapNumber: 0, Duration: 90 * time.Second, Timed: false, Complete: true}, // formation
		racingLap("s", 1, 90*time.Second),
		racingLap("s", 2, 90*time.Second),
		{SessionID: "s", LapNumber: 3, Duration: 90 * time.Second, Timed: true, Complete: false}, // partial
	}
	res, err := MedianReference(traces, "s", laps, []string{"speed"}, 5)
	if err != nil {
		t.Fatalf("MedianReference: %v", err)
	}
	if res.LapCount != 2 {
		t.Fatalf("LapCount = %d, want 2 (only full racing laps)", res.LapCount)
	}
	allEqual(t, "speed", res.Series["speed"], 110)
}

// TestMedianReference_NonAnalyzableExcluded: a racing lap whose trace span diverges from its duration
// is dropped by the SectorAnalyzable gate.
func TestMedianReference_NonAnalyzableExcluded(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(refTrace("s", 1, base, 90*time.Second, 100))
	// Lap 2: trace span 10s but duration 90s -> not analyzable, wild speed must not appear.
	_ = traces.SaveBatch(refTrace("s", 2, base, 10*time.Second, 9000))

	laps := []Lap{racingLap("s", 1, 90*time.Second), racingLap("s", 2, 90*time.Second)}
	res, err := MedianReference(traces, "s", laps, []string{"speed"}, 5)
	if err != nil {
		t.Fatalf("MedianReference: %v", err)
	}
	if res.LapCount != 1 {
		t.Fatalf("LapCount = %d, want 1 (non-analyzable lap dropped)", res.LapCount)
	}
	allEqual(t, "speed", res.Series["speed"], 100)
}

// TestMedianReference_ChannelFilter: requesting only "speed" yields just that key; grid length == points.
func TestMedianReference_ChannelFilter(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(refTrace("s", 1, base, 90*time.Second, 100))
	laps := []Lap{racingLap("s", 1, 90*time.Second)}

	res, err := MedianReference(traces, "s", laps, []string{"speed"}, 20)
	if err != nil {
		t.Fatalf("MedianReference: %v", err)
	}
	if len(res.Series) != 1 {
		t.Fatalf("Series has %d keys, want 1", len(res.Series))
	}
	if _, ok := res.Series["speed"]; !ok {
		t.Errorf("Series missing 'speed' key: %v", res.Series)
	}
	if len(res.Series["speed"]) != 20 || len(res.Fractions) != 20 {
		t.Errorf("lengths: series=%d fractions=%d, want 20", len(res.Series["speed"]), len(res.Fractions))
	}
}

// TestMedianReference_UnknownChannelFallsBackToAll: an unknown channel name is ignored; when the
// resolved set is empty the reference uses ALL continuous channels.
func TestMedianReference_UnknownChannelFallsBackToAll(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	traces := NewMemTraceStore()
	_ = traces.SaveBatch(refTrace("s", 1, base, 90*time.Second, 100))
	laps := []Lap{racingLap("s", 1, 90*time.Second)}

	res, err := MedianReference(traces, "s", laps, []string{"foo"}, 5)
	if err != nil {
		t.Fatalf("MedianReference: %v", err)
	}
	// All 11 continuous channels present.
	if len(res.Series) != 11 {
		t.Fatalf("Series has %d keys, want 11 (fallback to all)", len(res.Series))
	}
	for _, want := range []string{"speed", "throttle", "brake", "rpm", "fuel", "tyre_temp_fl", "tyre_temp_fr", "tyre_temp_rl", "tyre_temp_rr", "clutch", "boost"} {
		if _, ok := res.Series[want]; !ok {
			t.Errorf("Series missing %q", want)
		}
	}
}

// TestMedianReference_EmptyPool: no racing laps -> zero LapCount, non-nil per-channel slices, grid intact.
func TestMedianReference_EmptyPool(t *testing.T) {
	traces := NewMemTraceStore()
	res, err := MedianReference(traces, "s", nil, []string{"speed"}, 4)
	if err != nil {
		t.Fatalf("MedianReference: %v", err)
	}
	if res.LapCount != 0 {
		t.Errorf("LapCount = %d, want 0", res.LapCount)
	}
	if res.Series["speed"] == nil {
		t.Errorf("Series[speed] is nil, want non-nil empty-value slice")
	}
	if len(res.Fractions) != 4 {
		t.Errorf("Fractions len = %d, want 4", len(res.Fractions))
	}
}

// TestMedianReference_PointsClamp: points below the floor clamps to 2, above the ceiling to 5000.
func TestMedianReference_PointsClamp(t *testing.T) {
	traces := NewMemTraceStore()
	lo, _ := MedianReference(traces, "s", nil, []string{"speed"}, 1)
	if len(lo.Fractions) != 2 {
		t.Errorf("points=1 -> Fractions len %d, want 2", len(lo.Fractions))
	}
	hi, _ := MedianReference(traces, "s", nil, []string{"speed"}, 999999)
	if len(hi.Fractions) != 5000 {
		t.Errorf("points=999999 -> Fractions len %d, want 5000", len(hi.Fractions))
	}
}
