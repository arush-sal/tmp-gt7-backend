package processing

import "sort"

// ReferenceSeries is a per-channel median line across a session's valid racing laps, sampled on a
// shared distance-fraction grid.
type ReferenceSeries struct {
	Fractions []float64            `json:"fractions"` // grid points in [0,1], length = points
	Series    map[string][]float64 `json:"series"`    // channel -> median value per grid point
	LapCount  int                  `json:"lap_count"` // number of laps pooled
}

// refChannels maps a channel name to the continuous Sample field it reads (cast to float64). Order is
// not significant here — the resolved channel names are sorted so output is deterministic.
var refChannels = map[string]func(NormSample) float64{
	"speed":        func(ns NormSample) float64 { return float64(ns.Speed) },
	"throttle":     func(ns NormSample) float64 { return float64(ns.Throttle) },
	"brake":        func(ns NormSample) float64 { return float64(ns.Brake) },
	"rpm":          func(ns NormSample) float64 { return float64(ns.RPM) },
	"fuel":         func(ns NormSample) float64 { return float64(ns.Fuel) },
	"tyre_temp_fl": func(ns NormSample) float64 { return float64(ns.TyreTempFL) },
	"tyre_temp_fr": func(ns NormSample) float64 { return float64(ns.TyreTempFR) },
	"tyre_temp_rl": func(ns NormSample) float64 { return float64(ns.TyreTempRL) },
	"tyre_temp_rr": func(ns NormSample) float64 { return float64(ns.TyreTempRR) },
	"clutch":       func(ns NormSample) float64 { return float64(ns.Clutch) },
	"boost":        func(ns NormSample) float64 { return float64(ns.Boost) },
}

// MedianReference builds ReferenceSeries for the requested channels. Only FULL RACING laps pool into
// the median (Timed && Complete && !Fallback && SectorAnalyzable) so a formation/partial/spliced lap
// can't skew it (same gate as SessionLapSectors' best pool). Each pooled lap is Normalized and sampled
// at `points` evenly-spaced fractions via NormalizedLap.At; the median across laps is taken per grid
// point per channel (even count -> average of the two middle values). Empty pool -> zero LapCount and
// per-channel slices of zeros (non-nil, length points). Unknown channel names are ignored; when none
// are known, all continuous channels are used. points is clamped to [2, 5000].
//
// A lap whose ByLap read errors (or returns no samples) is skipped rather than failing the whole
// request — one bad lap should not blank a session's reference. The error return is reserved for a
// future truly-unexpected condition; today it is always nil.
func MedianReference(traces TraceStore, sessionID string, laps []Lap, channels []string, points int) (ReferenceSeries, error) {
	if points < 2 {
		points = 2
	}
	if points > 5000 {
		points = 5000
	}

	names := resolveChannels(channels)

	// fractions: points evenly spaced from 0 to 1 inclusive.
	fractions := make([]float64, points)
	for i := range fractions {
		fractions[i] = float64(i) / float64(points-1)
	}

	// collected[channel][gridPoint] accumulates one value per contributing lap at that grid point.
	collected := make(map[string][][]float64, len(names))
	for _, name := range names {
		collected[name] = make([][]float64, points)
	}

	lapCount := 0
	for _, l := range laps {
		if !l.Timed || !l.Complete || l.Fallback {
			continue
		}
		samples, err := traces.ByLap(sessionID, l.LapNumber)
		if err != nil || len(samples) == 0 {
			continue
		}
		if !SectorAnalyzable(l.Duration, samples) {
			continue
		}
		norm := Normalize(samples)
		lapCount++
		for gi, f := range fractions {
			ns, ok := norm.At(f)
			if !ok {
				continue
			}
			for _, name := range names {
				collected[name][gi] = append(collected[name][gi], refChannels[name](ns))
			}
		}
	}

	series := make(map[string][]float64, len(names))
	for _, name := range names {
		vals := make([]float64, points)
		for gi := range vals {
			vals[gi] = median(collected[name][gi]) // no contributor at this grid point -> 0
		}
		series[name] = vals
	}

	return ReferenceSeries{Fractions: fractions, Series: series, LapCount: lapCount}, nil
}

// resolveChannels filters requested to the known channel names; when the result is empty (none
// requested, or all unknown) it falls back to every channel. The returned names are sorted so the
// output key order is deterministic.
func resolveChannels(requested []string) []string {
	var names []string
	for _, c := range requested {
		if _, ok := refChannels[c]; ok {
			names = append(names, c)
		}
	}
	if len(names) == 0 {
		names = make([]string, 0, len(refChannels))
		for k := range refChannels {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return names
}

// median returns the middle value of vals (average of the two middles for an even count), or 0 for an
// empty slice. It sorts a copy so the caller's slice is left untouched.
func median(vals []float64) float64 {
	n := len(vals)
	if n == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
