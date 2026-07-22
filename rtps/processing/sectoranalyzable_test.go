package processing

import (
	"testing"
	"time"
)

// TestSectorAnalyzable mirrors the tolerance-boundary coverage that lived in the analysis package
// before SectorAnalyzable moved here: a clean full-trace lap passes; a lap whose wall-clock span
// diverges beyond tolerance fails; empty/single-sample/zero-distance laps fail.
func TestSectorAnalyzable(t *testing.T) {
	// clean lap: trace spans exactly the lap duration, distance axis present -> analyzable.
	clean := []Sample{tSample(0, 0, 100), tSample(100, 90, 120)}
	if !SectorAnalyzable(90*time.Second, clean) {
		t.Errorf("clean 90s lap (span 90s) should be analyzable")
	}

	// within jitter band: span off by less than SectorJitterTol -> still analyzable.
	within := []Sample{tSample(0, 0, 100), {Distance: 100, RecvTime: segBase.Add(90*time.Second + 100*time.Millisecond)}}
	if !SectorAnalyzable(90*time.Second, within) {
		t.Errorf("90s lap with 100ms span jitter should be analyzable (tol %v)", SectorJitterTol(90*time.Second))
	}

	// span diverges far beyond tolerance (join/splice lap): 92s duration, 15s span -> not analyzable.
	splice := []Sample{tSample(0, 0, 100), tSample(100, 15, 120)}
	if SectorAnalyzable(92*time.Second, splice) {
		t.Errorf("92s lap with 15s span must not be analyzable")
	}

	// degenerate cases.
	if SectorAnalyzable(0, clean) {
		t.Errorf("zero duration must not be analyzable")
	}
	if SectorAnalyzable(90*time.Second, nil) {
		t.Errorf("empty samples must not be analyzable")
	}
	if SectorAnalyzable(90*time.Second, clean[:1]) {
		t.Errorf("single sample must not be analyzable")
	}
	zeroDist := []Sample{tSample(0, 0, 100), tSample(0, 90, 120)}
	if SectorAnalyzable(90*time.Second, zeroDist) {
		t.Errorf("zero-distance (stationary) lap must not be analyzable")
	}
}

// TestSectorJitterTol: floor is the absolute tolerance; the percentage term wins for long laps.
func TestSectorJitterTol(t *testing.T) {
	if got := SectorJitterTol(10 * time.Second); got != sectorSpanAbsTol {
		t.Errorf("SectorJitterTol(10s) = %v, want floor %v", got, sectorSpanAbsTol)
	}
	// 0.5% of 100s = 500ms > 300ms floor.
	if got := SectorJitterTol(100 * time.Second); got != 500*time.Millisecond {
		t.Errorf("SectorJitterTol(100s) = %v, want 500ms", got)
	}
}
