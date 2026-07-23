package processing

import "sort"

// ExtremumKind distinguishes a speed peak (local maximum) from a valley (local minimum).
type ExtremumKind int

const (
	// Peak is a significant local speed maximum (a straight/exit crest).
	Peak ExtremumKind = iota
	// Valley is a significant local speed minimum (a corner apex).
	Valley
)

// String renders the kind as the lower-case token used in the JSON API ("peak"|"valley").
func (k ExtremumKind) String() string {
	if k == Peak {
		return "peak"
	}
	return "valley"
}

// Extremum is one significant local max/min of the speed trace, located on the 0..1 track-distance
// axis. Speed is read from the RAW (un-smoothed) sample so the reported value is the true fastest/
// slowest point, not a smoothing artefact.
type Extremum struct {
	Index    int          // ordinal along the lap (distance-sorted)
	Kind     ExtremumKind // Peak | Valley
	Fraction float64      // track fraction 0..1
	Distance float64      // cumulative in-lap metres
	Speed    float32      // km/h at the extremum (raw, un-smoothed)
	// DeltaNs is this lap's time vs the resolved reference at Fraction, in ns (positive == this lap is
	// slower/behind there). nil when no reliable reference exists — filled by AnnotateReferenceDelta,
	// NOT by detection.
	DeltaNs *int64
}

// Extrema is the significant peaks & valleys of one lap, distance-sorted and strictly alternating
// peak/valley.
type Extrema struct {
	Points []Extremum // distance-sorted, strictly alternating peak/valley
}

// ExtremaConfig tunes peak/valley detection. It mirrors CornerConfig so the two speed detectors share
// the same smoothing + prominence knobs. Zero value is usable via DefaultExtremaConfig.
type ExtremaConfig struct {
	SmoothWindow     int     // centered moving-average window on speed (samples); <=1 disables
	MinProminenceKmh float32 // two-sided topographic prominence required to keep an extremum
}

// DefaultExtremaConfig is a reasonable starting point (light smoothing, 15 km/h prominence), matching
// DefaultCornerConfig; pending tuning against real captures + the RKB.
func DefaultExtremaConfig() ExtremaConfig {
	return ExtremaConfig{SmoothWindow: 5, MinProminenceKmh: 15}
}

// candidate is an interior local extremum before alternation is enforced.
type candidate struct {
	idx        int          // index into the (smoothed) speed slice
	kind       ExtremumKind //
	prominence float32      // two-sided topographic prominence (see speedProminence)
}

// SpeedExtrema finds the significant speed peaks & valleys of a distance-normalized lap. It is the
// symmetric generalisation of DetectCorners: speed is optionally smoothed, interior local maxima AND
// minima are located, each is kept only if its two-sided topographic prominence meets
// MinProminenceKmh, each survivor is refined to the RAW local extreme in its bracket, and adjacency is
// reduced until the sequence strictly alternates. Fraction/Distance/Speed are read from the original
// (un-smoothed) samples. DeltaNs is always nil here — the reference join is a separate step
// (AnnotateReferenceDelta) so detection stays pure.
func SpeedExtrema(n NormalizedLap, cfg ExtremaConfig) Extrema {
	p := n.Points
	if len(p) < 3 {
		return Extrema{}
	}

	sp := make([]float32, len(p))
	for i := range p {
		sp[i] = p[i].Speed
	}
	if cfg.SmoothWindow > 1 {
		sp = movingAvg(sp, cfg.SmoothWindow)
	}

	// Interior local extrema. A plateau is counted once, at its LEADING edge — the minima predicate is
	// verbatim corner.go:50 and the maxima predicate is its exact negation, so the two detectors agree.
	var cands []candidate
	for i := 1; i < len(sp)-1; i++ {
		switch {
		case sp[i] < sp[i-1] && sp[i] <= sp[i+1]: // local minimum (falling edge of a valley plateau)
			cands = append(cands, candidate{idx: i, kind: Valley})
		case sp[i] > sp[i-1] && sp[i] >= sp[i+1]: // local maximum (rising edge of a peak plateau)
			cands = append(cands, candidate{idx: i, kind: Peak})
		}
	}

	// Prominence filter, per candidate, independently for peaks and valleys.
	kept := cands[:0:0]
	for _, c := range cands {
		c.prominence = speedProminence(sp, c.idx, c.kind)
		if c.prominence >= cfg.MinProminenceKmh {
			kept = append(kept, c)
		}
	}

	// Enforce strict alternation. Because peaks and valleys are filtered independently, two same-kind
	// candidates can survive adjacent when the opposite-kind candidate between them failed its own
	// prominence check. Collapse each adjacent same-kind pair by keeping the stronger (tie -> earlier).
	//
	// No prominence RE-computation is needed: speedProminence is topographic over the smoothed array
	// (it walks to the nearest strictly-lower/higher sample regardless of which candidates survive), so
	// a candidate's prominence is independent of the kept-list and cannot go stale when a neighbour is
	// dropped. The stored value stays correct for every subsequent comparison.
	kept = reduceToAlternating(kept)

	// Refine each survivor to the true RAW extreme within its bracket (its adjacent survivors, or the
	// lap ends) — smoothing can shift the detected index off the real crest/floor. Report the raw
	// sample's fraction/distance/speed.
	out := Extrema{Points: make([]Extremum, len(kept))}
	for j, c := range kept {
		lo, hi := 0, len(sp)-1
		if j > 0 {
			lo = kept[j-1].idx
		}
		if j < len(kept)-1 {
			hi = kept[j+1].idx
		}
		best := c.idx
		for k := lo; k <= hi; k++ {
			if c.kind == Valley && p[k].Speed < p[best].Speed {
				best = k
			}
			if c.kind == Peak && p[k].Speed > p[best].Speed {
				best = k
			}
		}
		out.Points[j] = Extremum{
			Index:    j,
			Kind:     c.kind,
			Fraction: p[best].Fraction,
			Distance: p[best].Distance,
			Speed:    p[best].Speed,
		}
	}
	// Detection produces candidates in ascending index order and the reduction/refinement preserve it,
	// but sort defensively so "distance-sorted" is a guaranteed post-condition.
	sort.SliceStable(out.Points, func(a, b int) bool { return out.Points[a].Distance < out.Points[b].Distance })
	for i := range out.Points {
		out.Points[i].Index = i
	}
	return out
}

// reduceToAlternating collapses runs of adjacent same-kind candidates (distance-sorted input) until
// the sequence strictly alternates, keeping the stronger of each colliding pair (tie -> the earlier,
// lower-index candidate). Prominences are stable (see SpeedExtrema), so a single left-to-right sweep
// repeated until no collision remains is well-defined and terminates (the list strictly shrinks).
func reduceToAlternating(in []candidate) []candidate {
	for {
		collision := -1
		for i := 1; i < len(in); i++ {
			if in[i].kind == in[i-1].kind {
				collision = i
				break
			}
		}
		if collision < 0 {
			return in
		}
		prev, cur := in[collision-1], in[collision]
		drop := collision // default: drop the later candidate (keeps the earlier on a tie)
		if cur.prominence > prev.prominence {
			drop = collision - 1
		}
		in = append(in[:drop], in[drop+1:]...)
	}
}

// speedProminence is the two-sided topographic prominence of the extremum at index i. For a Valley it
// is cornerProminence (the smaller of the two peaks reached before a strictly-lower sample, minus the
// valley speed). For a Peak it is the exact mirror: walk outward until a strictly-HIGHER sample,
// tracking the running MIN each side, and return the peak speed minus the LARGER of the two side-mins.
//
// The peak formula is spelled out rather than derived by a naive `<`->`>` swap: the correct mirror
// must also flip max->min AND the outer min(...)->max(...). Verified against the negation identity
// speedProminence(sp,i,Peak) == speedProminence(-sp,i,Valley) in the tests.
func speedProminence(sp []float32, i int, kind ExtremumKind) float32 {
	if kind == Valley {
		return cornerProminence(sp, i)
	}
	v := sp[i]
	leftMin := v
	for k := i - 1; k >= 0; k-- {
		if sp[k] > v { // a higher summit ends this peak's basin on the left
			break
		}
		if sp[k] < leftMin {
			leftMin = sp[k]
		}
	}
	rightMin := v
	for k := i + 1; k < len(sp); k++ {
		if sp[k] > v {
			break
		}
		if sp[k] < rightMin {
			rightMin = sp[k]
		}
	}
	return v - maxF32(leftMin, rightMin)
}

func maxF32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
