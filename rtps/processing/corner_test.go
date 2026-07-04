package processing

import (
	"math"
	"testing"
)

// speedLap builds a normalized lap from a speed profile with equally-spaced fractions (0..1).
func speedLap(speeds ...float32) NormalizedLap {
	samples := make([]Sample, len(speeds))
	for i, s := range speeds {
		samples[i] = Sample{Distance: float64(i), Speed: s}
	}
	return Normalize(samples)
}

func approxFrac(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestDetectCorners_TwoValleys: two prominent speed minima -> two corners with the right apexes and
// entry/exit peaks. No smoothing so the profile is exact.
func TestDetectCorners_TwoValleys(t *testing.T) {
	// idx:   0   1   2    3    4   5    6    7   8   (fractions i/8)
	lap := speedLap(200, 180, 120, 180, 200, 150, 100, 150, 200)
	corners := DetectCorners(lap, CornerConfig{SmoothWindow: 1, MinProminenceKmh: 15})

	if len(corners) != 2 {
		t.Fatalf("got %d corners, want 2", len(corners))
	}
	if corners[0].MinSpeed != 120 || corners[1].MinSpeed != 100 {
		t.Errorf("apex speeds = %v, %v, want 120, 100", corners[0].MinSpeed, corners[1].MinSpeed)
	}
	if !approxFrac(corners[0].ApexFraction, 2.0/8) || !approxFrac(corners[1].ApexFraction, 6.0/8) {
		t.Errorf("apex fractions = %v, %v, want .25, .75", corners[0].ApexFraction, corners[1].ApexFraction)
	}
	// Corner 0 is bracketed by the 200-peaks at idx0 and idx4.
	if !approxFrac(corners[0].EntryFraction, 0) || !approxFrac(corners[0].ExitFraction, 4.0/8) {
		t.Errorf("corner0 entry/exit = %v/%v, want 0/.5", corners[0].EntryFraction, corners[0].ExitFraction)
	}
}

// TestDetectCorners_FlatNoCorners: a flat speed profile has no corners.
func TestDetectCorners_FlatNoCorners(t *testing.T) {
	if c := DetectCorners(speedLap(200, 200, 200, 200, 200), DefaultCornerConfig()); len(c) != 0 {
		t.Errorf("flat lap: %d corners, want 0", len(c))
	}
}

// TestDetectCorners_ProminenceFilter: a shallow dip below the prominence threshold is not a corner.
func TestDetectCorners_ProminenceFilter(t *testing.T) {
	lap := speedLap(200, 190, 200) // 10 km/h dip
	if c := DetectCorners(lap, CornerConfig{SmoothWindow: 1, MinProminenceKmh: 15}); len(c) != 0 {
		t.Errorf("shallow dip: %d corners, want 0 (below prominence)", len(c))
	}
	if c := DetectCorners(lap, CornerConfig{SmoothWindow: 1, MinProminenceKmh: 5}); len(c) != 1 {
		t.Errorf("dip with low threshold: %d corners, want 1", len(c))
	}
}

// TestDetectCorners_Smoothing: a single real valley with sample noise still yields one corner when
// smoothed (noise spikes don't split it into several).
func TestDetectCorners_Smoothing(t *testing.T) {
	lap := speedLap(200, 198, 150, 152, 100, 151, 149, 199, 200)
	c := DetectCorners(lap, CornerConfig{SmoothWindow: 3, MinProminenceKmh: 15})
	if len(c) != 1 {
		t.Fatalf("got %d corners, want 1 (smoothed single valley)", len(c))
	}
	// Apex refined to the true raw minimum (idx4: speed 100, fraction 0.5) despite smoothing.
	if c[0].MinSpeed != 100 {
		t.Errorf("apex MinSpeed = %v, want 100 (refined to raw valley)", c[0].MinSpeed)
	}
	if !approxFrac(c[0].ApexFraction, 4.0/8) {
		t.Errorf("apex fraction = %v, want 0.5 (true valley, not smoothing-shifted)", c[0].ApexFraction)
	}
}

// TestDetectCorners_TooFewPoints: fewer than 3 samples -> no corners, no panic.
func TestDetectCorners_TooFewPoints(t *testing.T) {
	if c := DetectCorners(speedLap(100, 100), DefaultCornerConfig()); c != nil {
		t.Errorf("2-point lap: %v, want nil", c)
	}
}
