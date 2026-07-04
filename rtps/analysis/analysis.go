// Package analysis is the backend's Analysis domain (GARAGE domain 4). It is deterministic — no LLM
// performs telemetry analysis (an explanation layer converts these findings to prose much later). It
// consumes the canonical lap model + sectors (Processing) and produces Personal-Best selection,
// lap/sector deltas, consistency, and explainable recommendations.
package analysis

import (
	"math"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
)

// LapDelta is a valid lap's time relative to the Personal Best.
type LapDelta struct {
	LapNumber int32
	Duration  time.Duration
	DeltaToPB time.Duration // Duration - PB.Duration (0 for the PB lap itself)
}

// SectorDelta is one sector's time for a lap relative to the PB lap's same sector — where time is
// won or lost around the track.
type SectorDelta struct {
	Index     int
	LapTime   time.Duration
	PBTime    time.Duration
	DeltaToPB time.Duration // LapTime - PBTime (positive = slower than PB here)
}

// Consistency summarizes the spread of valid lap times.
type Consistency struct {
	Count  int
	Best   time.Duration
	Mean   time.Duration
	StdDev time.Duration
	Spread time.Duration // slowest - fastest valid lap
}

// PersonalBest returns the fastest PB-usable lap (Valid: timed, complete, gap-free). ok is false when
// there is no valid lap (e.g. an abandoned session).
func PersonalBest(laps []processing.Lap) (processing.Lap, bool) {
	var best processing.Lap
	found := false
	for _, l := range laps {
		if !l.Valid {
			continue
		}
		if !found || l.Duration < best.Duration {
			best, found = l, true
		}
	}
	return best, found
}

// consistencyOf computes spread stats over the given valid-lap durations.
func consistencyOf(laps []processing.Lap) Consistency {
	var c Consistency
	var durs []time.Duration
	for _, l := range laps {
		if l.Valid {
			durs = append(durs, l.Duration)
		}
	}
	c.Count = len(durs)
	if c.Count == 0 {
		return c
	}
	best, worst, sum := durs[0], durs[0], time.Duration(0)
	for _, d := range durs {
		if d < best {
			best = d
		}
		if d > worst {
			worst = d
		}
		sum += d
	}
	c.Best, c.Spread = best, worst-best
	mean := sum / time.Duration(c.Count)
	c.Mean = mean
	var varSum float64
	for _, d := range durs {
		diff := float64(d - mean)
		varSum += diff * diff
	}
	c.StdDev = time.Duration(math.Sqrt(varSum / float64(c.Count)))
	return c
}

// sectorDeltas aligns a lap's sectors with the PB lap's sectors by Index and computes per-sector
// deltas. Sectors present in only one of the two are skipped (they can't be compared).
func sectorDeltas(lap, pb []processing.SectorResult) []SectorDelta {
	pbByIndex := make(map[int]processing.SectorResult, len(pb))
	for _, s := range pb {
		pbByIndex[s.Index] = s
	}
	var out []SectorDelta
	for _, s := range lap {
		p, ok := pbByIndex[s.Index]
		if !ok {
			continue
		}
		out = append(out, SectorDelta{
			Index:     s.Index,
			LapTime:   s.Duration,
			PBTime:    p.Duration,
			DeltaToPB: s.Duration - p.Duration,
		})
	}
	return out
}
