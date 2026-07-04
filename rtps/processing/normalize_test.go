package processing

import (
	"math"
	"testing"
)

// sample is a tiny helper to build a trace Sample with just distance + speed + gear (the fields these
// tests exercise).
func sample(dist float64, speed float32, gear int32) Sample {
	return Sample{Distance: dist, Speed: speed, Gear: gear}
}

// TestNormalize_Fractions: fractions span 0..1 proportional to cumulative distance.
func TestNormalize_Fractions(t *testing.T) {
	n := Normalize([]Sample{sample(0, 0, 1), sample(25, 0, 2), sample(100, 0, 3)})
	if n.TotalDistance != 100 {
		t.Fatalf("TotalDistance = %v, want 100", n.TotalDistance)
	}
	want := []float64{0, 0.25, 1}
	for i, w := range want {
		if n.Points[i].Fraction != w {
			t.Errorf("Points[%d].Fraction = %v, want %v", i, n.Points[i].Fraction, w)
		}
	}
}

// TestNormalize_Empty / zero-distance: safe (no panic, no divide-by-zero).
func TestNormalize_EdgeCases(t *testing.T) {
	if n := Normalize(nil); len(n.Points) != 0 || n.TotalDistance != 0 {
		t.Errorf("empty: %+v", n)
	}
	// All samples at distance 0 -> fractions all 0.
	n := Normalize([]Sample{sample(0, 10, 1), sample(0, 20, 1)})
	if n.TotalDistance != 0 || n.Points[1].Fraction != 0 {
		t.Errorf("zero-distance: %+v", n)
	}
}

// TestAt_Interpolates: continuous fields interpolate linearly between bracketing samples; discrete
// fields take the earlier sample; f is clamped to [0,1].
func TestAt_Interpolates(t *testing.T) {
	n := Normalize([]Sample{sample(0, 100, 2), sample(50, 200, 3), sample(100, 300, 4)})

	cases := []struct {
		f         float64
		wantSpeed float32
		wantGear  int32
	}{
		{-1.0, 100, 2}, // clamp low -> first
		{0.0, 100, 2},  // first
		{0.25, 150, 2}, // between p0(f=0,g2) and p1(f=0.5,g3) -> speed 150, gear from earlier = 2
		{0.5, 200, 3},  // exact p1
		{0.75, 250, 3}, // between p1 and p2 -> gear from earlier = 3
		{1.0, 300, 4},  // last
		{2.0, 300, 4},  // clamp high -> last
	}
	for _, c := range cases {
		got, ok := n.At(c.f)
		if !ok {
			t.Fatalf("At(%v): ok=false", c.f)
		}
		if math.Abs(float64(got.Speed-c.wantSpeed)) > 1e-3 {
			t.Errorf("At(%v).Speed = %v, want %v", c.f, got.Speed, c.wantSpeed)
		}
		if got.Gear != c.wantGear {
			t.Errorf("At(%v).Gear = %d, want %d (discrete: earlier sample)", c.f, got.Gear, c.wantGear)
		}
	}
}

// TestAt_Empty: no points -> ok=false.
func TestAt_Empty(t *testing.T) {
	if _, ok := (NormalizedLap{}).At(0.5); ok {
		t.Error("At on empty lap: ok=true, want false")
	}
}
