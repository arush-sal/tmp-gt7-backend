package processing

import (
	"testing"
	"time"
)

// TestPredictedElapsed_Interpolation: elapsed synthetic-lap time is LINEARLY interpolated in TNs over
// Fraction between the two bracketing PredictedSamples (not snapped to the nearest), out-of-range
// fractions clamp to the endpoints, and a single-sample lap returns that sample's TNs everywhere.
func TestPredictedElapsed_Interpolation(t *testing.T) {
	pl := PredictedLap{Samples: []PredictedSample{
		{Fraction: 0.0, TNs: 0},
		{Fraction: 0.5, TNs: 1000},
		{Fraction: 1.0, TNs: 3000},
	}}
	fn := PredictedElapsed(pl)

	cases := []struct {
		f    float64
		want int64
	}{
		{0.0, 0},
		{0.25, 500},  // midway between 0 (0) and 0.5 (1000)
		{0.5, 1000},  // exact knot
		{0.75, 2000}, // midway between 0.5 (1000) and 1.0 (3000)
		{1.0, 3000},  // exact knot
		{-0.1, 0},    // clamp low
		{1.5, 3000},  // clamp high
	}
	for _, c := range cases {
		got, ok := fn(c.f)
		if !ok {
			t.Errorf("f=%v: ok=false, want true", c.f)
			continue
		}
		if got != c.want {
			t.Errorf("f=%v: TNs=%d, want %d", c.f, got, c.want)
		}
	}

	// len==1: any fraction returns the single sample's TNs.
	one := PredictedElapsed(PredictedLap{Samples: []PredictedSample{{Fraction: 0.3, TNs: 700}}})
	if got, ok := one(0.9); !ok || got != 700 {
		t.Errorf("single-sample: got %d ok=%v, want 700 true", got, ok)
	}

	// len==0: ok is false (empty reference).
	if _, ok := PredictedElapsed(PredictedLap{})(0.5); ok {
		t.Errorf("empty samples: ok=true, want false")
	}
}

// TestLapElapsed_MatchesTimeAt: lapElapsed equals TimeAt(f) minus the lap-start clock for a real lap,
// and its correctness depends on the first stored sample being the lap-start sample (Distance==0) —
// asserted here so a future truncated trace can't silently yield trace-elapsed instead of lap-elapsed.
func TestLapElapsed_MatchesTimeAt(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	samples := []Sample{
		{Distance: 0, RecvTime: base, Speed: 100},
		{Distance: 50, RecvTime: base.Add(1 * time.Second), Speed: 80},
		{Distance: 100, RecvTime: base.Add(3 * time.Second), Speed: 120},
	}
	n := Normalize(samples)
	if n.Points[0].Distance != 0 {
		t.Fatalf("Points[0].Distance=%v, want 0 (lap-start invariant lapElapsed relies on)", n.Points[0].Distance)
	}
	fn := LapElapsed(n)

	for _, f := range []float64{0, 0.25, 0.5, 0.75, 1.0} {
		got, ok := fn(f)
		if !ok {
			t.Fatalf("f=%v: ok=false", f)
		}
		ta, _ := n.TimeAt(f)
		want := ta.Sub(base).Nanoseconds()
		if got != want {
			t.Errorf("f=%v: lapElapsed=%d, want TimeAt-base=%d", f, got, want)
		}
	}

	// empty lap -> ok false.
	if _, ok := LapElapsed(NormalizedLap{})(0.5); ok {
		t.Errorf("empty lap: ok=true, want false")
	}
}

// TestAnnotateReferenceDelta_Sign: positive delta == queried lap behind the reference at f, negative ==
// ahead, ~0 == equal. Reference-agnostic: driven through a stub refElapsedFn while the queried lap's own
// elapsed comes from LapElapsed(queried).
func TestAnnotateReferenceDelta_Sign(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	// Queried lap elapsed at fraction 0.5 is exactly 1s (1e9 ns).
	queried := Normalize([]Sample{
		{Distance: 0, RecvTime: base},
		{Distance: 50, RecvTime: base.Add(1 * time.Second)},
		{Distance: 100, RecvTime: base.Add(2 * time.Second)},
	})
	const oneSec = int64(time.Second)

	tests := []struct {
		name    string
		refNs   int64
		wantPos bool // true -> expect delta > 0, false -> delta < 0
		zero    bool
	}{
		{"reference faster -> queried behind -> positive", oneSec - 200_000_000, true, false},
		{"reference slower -> queried ahead -> negative", oneSec + 200_000_000, false, false},
		{"reference equal -> zero", oneSec, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ex := Extrema{Points: []Extremum{{Kind: Valley, Fraction: 0.5}}}
			ref := func(f float64) (int64, bool) { return tc.refNs, true }
			AnnotateReferenceDelta(&ex, queried, ref)
			d := ex.Points[0].DeltaNs
			if d == nil {
				t.Fatalf("DeltaNs is nil, want a value")
			}
			switch {
			case tc.zero && *d != 0:
				t.Errorf("delta=%d, want 0", *d)
			case !tc.zero && tc.wantPos && *d <= 0:
				t.Errorf("delta=%d, want > 0", *d)
			case !tc.zero && !tc.wantPos && *d >= 0:
				t.Errorf("delta=%d, want < 0", *d)
			}
		})
	}
}

// TestAnnotateReferenceDelta_SelfZero: annotating against the queried lap's OWN elapsed (the mechanism
// behind ?reference={queried}) yields exactly-zero deltas — the numerator and reference share one
// lapElapsed constructor, so the self-reference guarantee is provable, not merely asserted.
func TestAnnotateReferenceDelta_SelfZero(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	queried := Normalize([]Sample{
		{Distance: 0, RecvTime: base},
		{Distance: 40, RecvTime: base.Add(1 * time.Second)},
		{Distance: 100, RecvTime: base.Add(2500 * time.Millisecond)},
	})
	ex := Extrema{Points: []Extremum{{Fraction: 0.2}, {Fraction: 0.6}, {Fraction: 0.95}}}
	AnnotateReferenceDelta(&ex, queried, LapElapsed(queried))
	for i, p := range ex.Points {
		if p.DeltaNs == nil || *p.DeltaNs != 0 {
			t.Errorf("point %d: delta=%v, want 0 (self-reference)", i, p.DeltaNs)
		}
	}
}

// TestAnnotateReferenceDelta_NoReference: a nil refElapsedFn leaves every DeltaNs nil — no fabricated
// zero delta when there is no usable reference.
func TestAnnotateReferenceDelta_NoReference(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	queried := Normalize([]Sample{
		{Distance: 0, RecvTime: base},
		{Distance: 100, RecvTime: base.Add(1 * time.Second)},
	})
	ex := Extrema{Points: []Extremum{{Fraction: 0.5}, {Fraction: 0.9}}}
	AnnotateReferenceDelta(&ex, queried, nil)
	for i, p := range ex.Points {
		if p.DeltaNs != nil {
			t.Errorf("point %d: delta=%v, want nil (no reference)", i, *p.DeltaNs)
		}
	}
}
