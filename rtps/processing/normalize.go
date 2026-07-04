package processing

// NormSample is a trace Sample placed on the lap's 0..1 track-distance axis.
type NormSample struct {
	Sample
	Fraction float64 // Sample.Distance / total lap distance, in [0,1]
}

// NormalizedLap is a lap's trace on a normalized track-distance axis. It lets two laps be compared by
// *where on track* they are (overlay by position, not time) — the foundation for sector splitting and
// lap-vs-lap deltas. GT7 gives no track distance, so distance was integrated at ingest (cumulative
// position deltas); Normalize turns that into a 0..1 fraction.
type NormalizedLap struct {
	Points        []NormSample
	TotalDistance float64
}

// Normalize maps a lap's ordered samples onto a fraction of total lap distance. Samples must be in
// traversal order (as TraceStore.ByLap returns them); cumulative Distance is monotonic, so the last
// sample's distance is the lap total. A zero-distance lap (single/stationary samples) is handled
// safely — all fractions 0, no divide-by-zero.
func Normalize(samples []Sample) NormalizedLap {
	n := NormalizedLap{Points: make([]NormSample, len(samples))}
	if len(samples) == 0 {
		return n
	}
	n.TotalDistance = samples[len(samples)-1].Distance
	for i, s := range samples {
		f := 0.0
		if n.TotalDistance > 0 {
			f = s.Distance / n.TotalDistance
		}
		n.Points[i] = NormSample{Sample: s, Fraction: f}
	}
	return n
}

// At returns the lap state at track-distance fraction f, linearly interpolating the continuous fields
// between the two bracketing samples. f is clamped to [0,1]; ok is false only when the lap has no
// points. This is how two laps are sampled at the same track position for delta comparison.
//
// Ties: when multiple samples share a fraction (a stationary stretch), the bracket search lands on the
// last such sample as the lower bound; an exact hit on the first knot returns the first sample. Either
// way the result is one of the real samples at that fraction, consistent with "discrete fields from
// the earlier sample".
func (n NormalizedLap) At(f float64) (NormSample, bool) {
	if len(n.Points) == 0 {
		return NormSample{}, false
	}
	if f <= n.Points[0].Fraction {
		return n.Points[0], true
	}
	last := n.Points[len(n.Points)-1]
	if f >= last.Fraction {
		return last, true
	}
	// Binary-search the bracket [lo, hi] with Points[lo].Fraction <= f < Points[hi].Fraction.
	lo, hi := 0, len(n.Points)-1
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if n.Points[mid].Fraction <= f {
			lo = mid
		} else {
			hi = mid
		}
	}
	a, b := n.Points[lo], n.Points[hi]
	t := 0.0
	if span := b.Fraction - a.Fraction; span > 0 {
		t = (f - a.Fraction) / span
	}
	return lerp(a, b, t, f), true
}

// lerp linearly interpolates the continuous fields of a and b at parameter t in [0,1], stamping the
// requested Fraction. Discrete/identity fields (SessionID, LapNumber, Seq, Gear, RecvTime) take the
// earlier sample a — interpolating a gear or a timestamp is meaningless.
func lerp(a, b NormSample, t, fraction float64) NormSample {
	out := a // copies discrete/identity fields from a
	out.Fraction = fraction
	out.Distance = a.Distance + (b.Distance-a.Distance)*t
	out.PosX = f32lerp(a.PosX, b.PosX, t)
	out.PosY = f32lerp(a.PosY, b.PosY, t)
	out.PosZ = f32lerp(a.PosZ, b.PosZ, t)
	out.Speed = f32lerp(a.Speed, b.Speed, t)
	out.Throttle = f32lerp(a.Throttle, b.Throttle, t)
	out.Brake = f32lerp(a.Brake, b.Brake, t)
	out.RPM = f32lerp(a.RPM, b.RPM, t)
	out.Yaw = f32lerp(a.Yaw, b.Yaw, t)
	out.AngVelY = f32lerp(a.AngVelY, b.AngVelY, t)
	return out
}

// f32lerp linearly interpolates two float32s at t. The whole computation is done in float64 so the
// difference can't overflow to Inf near float32 limits (not a risk for GT7 ranges, but cheap safety).
func f32lerp(x, y float32, t float64) float32 {
	return float32(float64(x) + (float64(y)-float64(x))*t)
}
