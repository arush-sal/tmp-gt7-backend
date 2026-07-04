package processing

// Corner is one detected corner of a lap, located on the 0..1 track-distance axis. Corners are
// derived from the telemetry trace (GT7 gives no track/corner data): an apex is a prominent local
// speed minimum, bracketed by the surrounding speed peaks (entry/exit). This is a heuristic v1 —
// thresholds are tunable and it will be validated/enriched against real captures + the RKB later.
type Corner struct {
	Index         int
	EntryFraction float64 // track fraction of the pre-apex speed peak (braking point-ish)
	ApexFraction  float64 // track fraction of the speed minimum
	ExitFraction  float64 // track fraction of the post-apex speed peak
	ApexDistance  float64 // cumulative in-lap metres at the apex
	MinSpeed      float32 // km/h at the apex
}

// CornerConfig tunes corner detection. Zero value is usable via DefaultCornerConfig.
type CornerConfig struct {
	SmoothWindow     int     // centered moving-average window on speed (samples); <=1 disables
	MinProminenceKmh float32 // required speed rise on BOTH sides of an apex to count as a corner
}

// DefaultCornerConfig is a reasonable starting point (light smoothing, 15 km/h prominence) pending
// tuning against real captures.
func DefaultCornerConfig() CornerConfig {
	return CornerConfig{SmoothWindow: 5, MinProminenceKmh: 15}
}

// DetectCorners finds corners in a distance-normalized lap. Speed is optionally smoothed, local
// minima are located, and each is kept only if its two-sided prominence (rise to the neighbouring
// peaks) meets MinProminenceKmh — filtering out noise wiggles and straights. Fractions/distance/speed
// are read from the original (un-smoothed) samples.
func DetectCorners(n NormalizedLap, cfg CornerConfig) []Corner {
	p := n.Points
	if len(p) < 3 {
		return nil
	}

	sp := make([]float32, len(p))
	for i := range p {
		sp[i] = p[i].Speed
	}
	if cfg.SmoothWindow > 1 {
		sp = movingAvg(sp, cfg.SmoothWindow)
	}

	// Local minima (interior points lower than a neighbour on each side; plateau counted at its
	// falling edge).
	var mins []int
	for i := 1; i < len(sp)-1; i++ {
		if sp[i] < sp[i-1] && sp[i] <= sp[i+1] {
			mins = append(mins, i)
		}
	}

	// Pass 1: keep only minima whose TOPOGRAPHIC prominence (climb to a peak before reaching a lower
	// minimum or the lap end) meets the threshold — this survives a bumpy valley floor that a naive
	// adjacent-minima comparison would split into cancelling shallow dips.
	kept := mins[:0:0]
	for _, mi := range mins {
		if cornerProminence(sp, mi) >= cfg.MinProminenceKmh {
			kept = append(kept, mi)
		}
	}

	// Pass 2: entry/exit are the peaks between an apex and its adjacent KEPT minima (or the lap ends).
	// Using kept (not all) minima means a rejected noise dip doesn't truncate the bracket short of the
	// real braking/exit peak.
	corners := make([]Corner, 0, len(kept))
	for j, mi := range kept {
		left, right := 0, len(sp)-1
		if j > 0 {
			left = kept[j-1]
		}
		if j < len(kept)-1 {
			right = kept[j+1]
		}
		lpi := argmaxRange(sp, left, mi)
		rpi := argmaxRange(sp, mi, right)
		// Refine the apex to the true RAW minimum within the bracket: smoothing can shift the detected
		// index off the real valley, so report the un-smoothed slowest point for accurate apex
		// location + min speed.
		apex := mi
		for k := lpi; k <= rpi; k++ {
			if p[k].Speed < p[apex].Speed {
				apex = k
			}
		}
		corners = append(corners, Corner{
			Index:         j,
			EntryFraction: p[lpi].Fraction,
			ApexFraction:  p[apex].Fraction,
			ExitFraction:  p[rpi].Fraction,
			ApexDistance:  p[apex].Distance,
			MinSpeed:      p[apex].Speed,
		})
	}
	return corners
}

// cornerProminence is the topographic prominence of the local minimum at index i: the smaller of the
// two peaks reached by walking outward until a strictly-lower sample (a deeper minimum) or the lap
// end, minus the minimum's speed. A minimum on a slope (an immediately-lower neighbour) has prominence
// 0. High prominence == a real, isolated corner.
func cornerProminence(sp []float32, i int) float32 {
	v := sp[i]
	leftMax := v
	for k := i - 1; k >= 0; k-- {
		if sp[k] < v {
			break
		}
		if sp[k] > leftMax {
			leftMax = sp[k]
		}
	}
	rightMax := v
	for k := i + 1; k < len(sp); k++ {
		if sp[k] < v {
			break
		}
		if sp[k] > rightMax {
			rightMax = sp[k]
		}
	}
	return minF32(leftMax, rightMax) - v
}

// movingAvg returns a centered moving average of xs. The effective window is 2*(window/2)+1, so an
// even window rounds up to the next odd size; edges are clamped to the available samples.
func movingAvg(xs []float32, window int) []float32 {
	if window < 2 {
		return xs
	}
	half := window / 2
	out := make([]float32, len(xs))
	for i := range xs {
		lo, hi := i-half, i+half
		if lo < 0 {
			lo = 0
		}
		if hi >= len(xs) {
			hi = len(xs) - 1
		}
		var sum float32
		for j := lo; j <= hi; j++ {
			sum += xs[j]
		}
		out[i] = sum / float32(hi-lo+1)
	}
	return out
}

// argmaxRange returns the index of the maximum value in xs[lo..hi] (inclusive).
func argmaxRange(xs []float32, lo, hi int) int {
	best := lo
	for i := lo + 1; i <= hi; i++ {
		if xs[i] > xs[best] {
			best = i
		}
	}
	return best
}

func minF32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
