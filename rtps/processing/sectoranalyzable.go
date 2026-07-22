package processing

import "time"

// Sector-analyzability tolerance. A lap's trace is trusted for sector timing only when its wall-clock
// span sits within this band of the lap's authoritative GT7 duration. Because contiguous mini-sectors
// telescope (Σ sector times == lastSample − firstSample == trace span), a span that disagrees with the
// real lap time means the trace does not cover exactly one clean lap. Clean 60Hz laps matched their
// GT7 time within ~0.1s across the 4-race capture; the floor leaves margin for the one-frame boundary
// (the next lap's first frame — which GT7 stamps the lap time from — is not in this lap's trace).
// Deliberately strict; widen only with evidence. (Mid-lap dropouts are already excluded upstream:
// Lap.Valid requires gapCount == 0.)
const (
	sectorSpanAbsTol = 300 * time.Millisecond
	sectorSpanPctTol = 0.005 // 0.5% of the lap duration
)

// SectorAnalyzable reports whether a valid lap's trace faithfully covers it, so its distance-fraction
// sector times reconcile with the lap's authoritative duration. It rejects traces that are empty, have
// no usable distance axis, or whose wall-clock span diverges from the lap duration beyond the tolerance
// — the join-lap / standing-start / splice cases whose sector times would otherwise collapse the
// predicted best.
func SectorAnalyzable(dur time.Duration, samples []Sample) bool {
	if dur <= 0 || len(samples) < 2 {
		return false
	}
	if samples[len(samples)-1].Distance <= 0 { // stationary/degenerate: no distance axis to split on
		return false
	}
	span := samples[len(samples)-1].RecvTime.Sub(samples[0].RecvTime)
	diff := span - dur
	if diff < 0 {
		diff = -diff
	}
	return diff <= SectorJitterTol(dur)
}

// SectorJitterTol is the wall-clock jitter band trace-derived sector times are allowed to diverge from
// a lap's authoritative GT7 duration by (see SectorAnalyzable's doc comment for why this exists —
// contiguous mini-sectors telescope to the trace span, and the trace span never lands exactly on the
// game-clock duration even for a clean lap). Shared with the BUG-4 PredictedBest<=PBTime guard: a
// predicted best built from the PB lap's own sectors carries the same jitter, so it must be judged
// against the same tolerance SectorAnalyzable already uses to admit that lap's sectors in the first
// place — not a hard zero-tolerance comparison, which would suppress essentially every prediction.
func SectorJitterTol(dur time.Duration) time.Duration {
	tol := sectorSpanAbsTol
	if p := time.Duration(sectorSpanPctTol * float64(dur)); p > tol {
		tol = p
	}
	return tol
}
