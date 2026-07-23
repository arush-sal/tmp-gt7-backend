package processing

import (
	"testing"
)

// TestSpeedExtrema_TooFewPoints: fewer than 3 samples cannot hold an interior extremum -> empty, no
// panic. Mirrors TestDetectCorners_TooFewPoints.
func TestSpeedExtrema_TooFewPoints(t *testing.T) {
	if got := SpeedExtrema(speedLap(100, 90), DefaultExtremaConfig()); len(got.Points) != 0 {
		t.Fatalf("2-point lap: %d points, want 0", len(got.Points))
	}
}

// TestSpeedExtrema_FlatNoExtrema: a flat speed profile has no extrema.
func TestSpeedExtrema_FlatNoExtrema(t *testing.T) {
	if got := SpeedExtrema(speedLap(200, 200, 200, 200, 200), DefaultExtremaConfig()); len(got.Points) != 0 {
		t.Errorf("flat lap: %d points, want 0", len(got.Points))
	}
}

// TestSpeedExtrema_AlternatingPeaksValleys: two prominent valleys with an interior peak between them
// -> strictly alternating valley/peak/valley on the 0..1 axis. Endpoints (idx 0, last) are NOT
// interior, so the flanking 200-peaks are not reported. No smoothing so the profile is exact.
func TestSpeedExtrema_AlternatingPeaksValleys(t *testing.T) {
	// idx:  0    1    2    3    4    5    6    7    8   (fractions i/8)
	lap := speedLap(200, 180, 120, 180, 200, 150, 100, 150, 200)
	ex := SpeedExtrema(lap, ExtremaConfig{SmoothWindow: 1, MinProminenceKmh: 15})

	wantKind := []ExtremumKind{Valley, Peak, Valley}
	wantFrac := []float64{2.0 / 8, 4.0 / 8, 6.0 / 8}
	wantSpeed := []float32{120, 200, 100}
	if len(ex.Points) != len(wantKind) {
		t.Fatalf("got %d points, want %d: %+v", len(ex.Points), len(wantKind), ex.Points)
	}
	for i, p := range ex.Points {
		if p.Index != i {
			t.Errorf("point %d: Index=%d, want %d", i, p.Index, i)
		}
		if p.Kind != wantKind[i] {
			t.Errorf("point %d: Kind=%s, want %s", i, p.Kind, wantKind[i])
		}
		if !approxFrac(p.Fraction, wantFrac[i]) {
			t.Errorf("point %d: Fraction=%v, want %v", i, p.Fraction, wantFrac[i])
		}
		if p.Speed != wantSpeed[i] {
			t.Errorf("point %d: Speed=%v, want %v", i, p.Speed, wantSpeed[i])
		}
		if p.DeltaNs != nil {
			t.Errorf("point %d: DeltaNs=%v, want nil (detection is pure)", i, *p.DeltaNs)
		}
	}
}

// TestSpeedExtrema_ProminenceFilter: a shallow dip below threshold is dropped; the same dip is kept
// under a lower threshold. Mirrors TestDetectCorners_ProminenceFilter.
func TestSpeedExtrema_ProminenceFilter(t *testing.T) {
	lap := speedLap(200, 190, 200) // 10 km/h dip
	if ex := SpeedExtrema(lap, ExtremaConfig{SmoothWindow: 1, MinProminenceKmh: 15}); len(ex.Points) != 0 {
		t.Errorf("shallow dip: %d points, want 0 (below prominence)", len(ex.Points))
	}
	ex := SpeedExtrema(lap, ExtremaConfig{SmoothWindow: 1, MinProminenceKmh: 5})
	if len(ex.Points) != 1 || ex.Points[0].Kind != Valley {
		t.Fatalf("dip with low threshold: %+v, want 1 valley", ex.Points)
	}
}

// TestSpeedExtrema_Smoothing: a single real valley with sample noise yields one valley, refined to the
// true RAW slowest sample (100 @ fraction .5), not the smoothing-shifted index/value. The noise wiggle
// does not survive as a spurious peak. Mirrors TestDetectCorners_Smoothing.
func TestSpeedExtrema_Smoothing(t *testing.T) {
	lap := speedLap(200, 198, 150, 152, 100, 151, 149, 199, 200)
	ex := SpeedExtrema(lap, ExtremaConfig{SmoothWindow: 3, MinProminenceKmh: 15})
	if len(ex.Points) != 1 {
		t.Fatalf("got %d points, want 1 (smoothed single valley): %+v", len(ex.Points), ex.Points)
	}
	p := ex.Points[0]
	if p.Kind != Valley {
		t.Errorf("Kind=%s, want valley", p.Kind)
	}
	if p.Speed != 100 {
		t.Errorf("Speed=%v, want 100 (refined to raw valley)", p.Speed)
	}
	if !approxFrac(p.Fraction, 4.0/8) {
		t.Errorf("Fraction=%v, want 0.5 (true valley, not smoothing-shifted)", p.Fraction)
	}
}

// TestSpeedExtrema_ValleyPlateau: a flat valley floor is counted once, at its LEADING edge.
func TestSpeedExtrema_ValleyPlateau(t *testing.T) {
	// idx: 0   1   2   3   (fractions i/3); valley floor 100 spans idx1-2.
	ex := SpeedExtrema(speedLap(200, 100, 100, 200), ExtremaConfig{SmoothWindow: 1, MinProminenceKmh: 15})
	if len(ex.Points) != 1 || ex.Points[0].Kind != Valley {
		t.Fatalf("%+v, want 1 valley", ex.Points)
	}
	if !approxFrac(ex.Points[0].Fraction, 1.0/3) {
		t.Errorf("Fraction=%v, want 1/3 (leading edge of plateau)", ex.Points[0].Fraction)
	}
}

// TestSpeedExtrema_PeakPlateau: a flat peak top is counted once, at its LEADING edge. This is the case
// the negation-identity test cannot catch (every index on a flat top has equal value/prominence), so
// assert the emitted index location directly.
func TestSpeedExtrema_PeakPlateau(t *testing.T) {
	// idx: 0   1   2   3   (fractions i/3); peak top 200 spans idx1-2.
	ex := SpeedExtrema(speedLap(100, 200, 200, 100), ExtremaConfig{SmoothWindow: 1, MinProminenceKmh: 15})
	if len(ex.Points) != 1 || ex.Points[0].Kind != Peak {
		t.Fatalf("%+v, want 1 peak", ex.Points)
	}
	if !approxFrac(ex.Points[0].Fraction, 1.0/3) {
		t.Errorf("Fraction=%v, want 1/3 (leading edge of plateau)", ex.Points[0].Fraction)
	}
}

// TestSpeedExtrema_AlternationReduction: two equally-prominent peaks flank a dip whose prominence is
// below threshold. The dip-valley is dropped, leaving two adjacent same-kind peaks; the reduction keeps
// one (tie -> earlier) so the result strictly alternates.
func TestSpeedExtrema_AlternationReduction(t *testing.T) {
	// idx: 0   1   2   3   4 ; peaks 200 @ idx1,idx3; dip 188 @ idx2 (prominence 12 < 15).
	ex := SpeedExtrema(speedLap(100, 200, 188, 200, 100), ExtremaConfig{SmoothWindow: 1, MinProminenceKmh: 15})
	if len(ex.Points) != 1 || ex.Points[0].Kind != Peak {
		t.Fatalf("%+v, want 1 peak (adjacent twin peaks reduced)", ex.Points)
	}
	if !approxFrac(ex.Points[0].Fraction, 1.0/4) {
		t.Errorf("Fraction=%v, want 1/4 (earlier of the tied twin peaks)", ex.Points[0].Fraction)
	}
}

// TestSpeedExtrema_PeakProminenceNegationIdentity: the peak-prominence branch must equal the valley
// branch evaluated on the negated signal at every interior index. Guards the mirror against a naive
// `<`->`>` swap that forgets to also flip max<->min.
func TestSpeedExtrema_PeakProminenceNegationIdentity(t *testing.T) {
	sp := []float32{200, 180, 120, 180, 205, 150, 100, 151, 149, 199, 200, 60, 210}
	neg := make([]float32, len(sp))
	for i, v := range sp {
		neg[i] = -v
	}
	for i := 1; i < len(sp)-1; i++ {
		peak := speedProminence(sp, i, Peak)
		valleyOfNeg := speedProminence(neg, i, Valley)
		if peak != valleyOfNeg {
			t.Errorf("idx %d: peakProm(sp)=%v, valleyProm(-sp)=%v (must be equal by negation)", i, peak, valleyOfNeg)
		}
	}
}

// TestSpeedExtrema_CornerConsistency: the two speed detectors must not drift apart. Every DetectCorners
// apex (a prominent speed minimum) must appear as a SpeedExtrema valley at the same fraction — corners'
// valleys are a subset of ours.
func TestSpeedExtrema_CornerConsistency(t *testing.T) {
	lap := speedLap(200, 180, 120, 180, 200, 150, 100, 150, 200)
	cfg := ExtremaConfig{SmoothWindow: 1, MinProminenceKmh: 15}
	corners := DetectCorners(lap, CornerConfig(cfg))
	ex := SpeedExtrema(lap, cfg)

	for _, c := range corners {
		found := false
		for _, p := range ex.Points {
			if p.Kind == Valley && approxFrac(p.Fraction, c.ApexFraction) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("corner apex @ fraction %v has no matching SpeedExtrema valley", c.ApexFraction)
		}
	}
}
