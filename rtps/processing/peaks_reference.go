package processing

// RefElapsedFn reports elapsed lap time in ns (since fraction 0) at track fraction f. ok is false only
// for an empty reference. Both reference kinds (a PredictedLap via PredictedElapsed, a real lap via
// LapElapsed) implement it, so AnnotateReferenceDelta is reference-agnostic. The API layer resolves
// which kind to build from the ?reference= param and passes the resulting fn here.
type RefElapsedFn func(f float64) (ns int64, ok bool)

// PredictedElapsed adapts a PredictedLap to a RefElapsedFn. A PredictedLap's Samples are
// []PredictedSample (NOT a NormalizedLap — no TimeAt), each carrying Fraction + TNs (elapsed synthetic-
// lap time, already measured since fraction 0). TNs is interpolated linearly over Fraction between the
// bracketing samples. Fraction is non-decreasing across Samples (contiguous ascending sectors, so a
// binary search is valid); out-of-range f clamps to the endpoint, len==1 returns that sample's TNs, and
// ok is false only when there are no samples.
//
// It deliberately does NOT fabricate a NormalizedLap from the predicted samples: their Distance is
// re-accumulated across splice seams and their RecvTime is not a real clock (predictedlap.go), so only
// Fraction + TNs are trustworthy for this join.
func PredictedElapsed(pl PredictedLap) RefElapsedFn {
	s := pl.Samples
	return func(f float64) (int64, bool) {
		if len(s) == 0 {
			return 0, false
		}
		if f <= s[0].Fraction {
			return s[0].TNs, true
		}
		last := len(s) - 1
		if f >= s[last].Fraction {
			return s[last].TNs, true
		}
		// Binary search for the bracket [lo, hi] with s[lo].Fraction <= f < s[hi].Fraction.
		lo, hi := 0, last
		for lo+1 < hi {
			mid := (lo + hi) / 2
			if s[mid].Fraction <= f {
				lo = mid
			} else {
				hi = mid
			}
		}
		span := s[hi].Fraction - s[lo].Fraction
		if span <= 0 {
			return s[lo].TNs, true
		}
		t := (f - s[lo].Fraction) / span
		return s[lo].TNs + int64(float64(s[hi].TNs-s[lo].TNs)*t), true
	}
}

// LapElapsed adapts a real NormalizedLap to a RefElapsedFn: elapsed time since lap start at fraction f
// is TimeAt(f) minus the first sample's clock. ok is false only for an empty lap.
//
// Correctness depends on the first stored sample being the lap-start sample (Distance == 0): elapsed is
// measured from Points[0].RecvTime, and TimeAt clamps f<=Points[0].Fraction to exactly that clock, so
// lapElapsed(0)==0. That first-sample-is-lap-start property is a trace-recording guarantee, not
// something Normalize enforces — a truncated/partial trace passed here would yield trace-elapsed, not
// lap-elapsed.
func LapElapsed(n NormalizedLap) RefElapsedFn {
	return func(f float64) (int64, bool) {
		if len(n.Points) == 0 {
			return 0, false
		}
		ta, ok := n.TimeAt(f)
		if !ok {
			return 0, false
		}
		return ta.Sub(n.Points[0].RecvTime).Nanoseconds(), true
	}
}

// AnnotateReferenceDelta fills each extremum's DeltaNs in place with the queried lap's time relative to
// a reference at that track fraction: DeltaNs = queriedElapsed(f) - ref(f). Positive means the queried
// lap is BEHIND (slower at) the reference there. It is reference-agnostic (ref is either kind's
// refElapsedFn) and keeps detection pure — SpeedExtrema never sets DeltaNs.
//
// A nil ref (no usable reference) leaves every DeltaNs nil — never a fabricated zero. Because both the
// numerator and (for reference B) the reference are produced by the same LapElapsed constructor,
// annotating against LapElapsed(queried) itself yields exactly-zero deltas (the self-reference case).
func AnnotateReferenceDelta(ex *Extrema, queried NormalizedLap, ref RefElapsedFn) {
	if ref == nil {
		return
	}
	q := LapElapsed(queried)
	for i := range ex.Points {
		f := ex.Points[i].Fraction
		qNs, okq := q(f)
		rNs, okr := ref(f)
		if !okq || !okr {
			continue // leave DeltaNs nil rather than fabricate a value
		}
		d := qNs - rNs
		ex.Points[i].DeltaNs = &d
	}
}
