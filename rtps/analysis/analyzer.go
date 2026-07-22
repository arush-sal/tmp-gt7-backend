package analysis

import (
	"context"
	"fmt"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
)

// LapAnalysis is one valid lap's analysis relative to the session PB.
type LapAnalysis struct {
	LapNumber    int32         `json:"lap_number"`
	Duration     time.Duration `json:"duration_ns"`
	DeltaToPB    time.Duration `json:"delta_to_pb_ns"`
	SectorDeltas []SectorDelta `json:"sector_deltas"`
}

// SessionAnalysis is the deterministic analysis of one session: its PB, per-lap and per-sector deltas,
// consistency, and explainable recommendations.
type SessionAnalysis struct {
	SessionID string        `json:"session_id"`
	HasPB     bool          `json:"has_pb"`
	PBLap     int32         `json:"pb_lap"`
	PBTime    time.Duration `json:"pb_time_ns"`
	Laps      []LapAnalysis `json:"laps"`
	// Consistency = how repeatable the valid laps are (own-PB comparison).
	Consistency Consistency `json:"consistency"`
	// Improvement view: the predicted-best lap (sum of the fastest sector times the driver has
	// achieved — the fastest lap achievable given observed performance, not an idealized/reference-
	// derived "theoretical" best) and how much time that leaves on the table vs the PB's sectors.
	PredictedBest   time.Duration `json:"predicted_best_ns"`
	TimeOnTable     time.Duration `json:"time_on_table_ns"`
	BestSectors     []SectorBest  `json:"best_sectors"`
	Recommendations []string      `json:"recommendations"`
}

// Analyzer produces SessionAnalysis from the stored lap model + traces + the RKB. It reads through the
// same interfaces the rest of the backend uses, so it works with in-memory or Postgres stores and a
// real or stub KB.
type Analyzer struct {
	Laps            processing.LapStore
	Traces          processing.TraceStore
	KB              reference.KB
	FallbackSectors int // equal mini-sector count used when the RKB can't identify the track
}

// AnalyzeSession computes the analysis for one session against the authoritative GT7 best lap time
// (gt7Best — the game's own record, immune to a dropped packet or a store miss on the actual fastest
// lap). PB is always gt7Best; PBLap is the eligible captured lap whose duration matches it within
// tolerance (0 when no captured lap matches — the fastest lap was not captured cleanly). For each
// eligible lap this computes its delta to PB and per-sector deltas (via the distance-normalized trace +
// RKB sectors), plus consistency and recommendations.
func (a Analyzer) AnalyzeSession(ctx context.Context, sessionID string, gt7Best time.Duration) (SessionAnalysis, error) {
	lapPtrs := a.Laps.BySession(sessionID)
	laps := make([]processing.Lap, 0, len(lapPtrs))
	for _, p := range lapPtrs {
		if p == nil { // defensive: stores return non-nil, but don't panic if that ever changes
			continue
		}
		laps = append(laps, *p)
	}
	res := SessionAnalysis{SessionID: sessionID, Consistency: consistencyOf(laps, gt7Best)}

	if gt7Best <= 0 {
		res.Recommendations = []string{"No completed lap yet — no Personal Best to analyze."}
		return res, nil
	}
	res.HasPB = true
	res.PBTime = gt7Best
	res.PBLap = matchLap(laps, gt7Best) // 0 when no captured lap matches the authoritative best

	// Compute each duration-eligible lap's sectors once; reuse for deltas + the predicted best. A lap
	// is only *sector-analyzable* if its trace faithfully covers it (see sectorAnalyzable): sector times
	// are derived from frame timestamps along the distance axis and telescope to the trace's wall-clock
	// span, so a lap whose trace does not span its authoritative duration (a mid-race join lap, a
	// standing-start spillover, a dropout splice) yields sector times that would corrupt every consumer
	// below. Such laps stay in PB/consistency (duration-based) but are omitted here, so they contribute
	// no best sector and get no sector deltas.
	perLap := make(map[int32][]processing.SectorResult)
	for _, l := range laps {
		if !l.Timed || !l.Complete || l.Fallback { // duration-eligible; sector trust handled by sectorAnalyzable
			continue
		}
		samples, err := a.Traces.ByLap(sessionID, l.LapNumber)
		if err != nil {
			return res, err
		}
		if !processing.SectorAnalyzable(l.Duration, samples) {
			continue
		}
		norm := processing.Normalize(samples)
		secs, _, err := processing.SectorsForLap(ctx, a.KB, sessionID, norm, a.FallbackSectors)
		if err != nil {
			return res, err
		}
		perLap[l.LapNumber] = secs
	}
	pbSectors := perLap[res.PBLap] // empty when PB lap unknown/unanalyzable

	// Improvement view: predicted best = sum of the fastest sector times; time on the table = how
	// much the PB's sectors trail those bests (both sector-time based, so apples-to-apples).
	res.PredictedBest, res.BestSectors = predictedBest(perLap)
	var pbSectorSum time.Duration
	for _, sd := range pbSectors {
		pbSectorSum += sd.Duration
	}
	if pbSectorSum > res.PredictedBest {
		res.TimeOnTable = pbSectorSum - res.PredictedBest
	}

	// BUG-4 guard: predicted best is only meaningful when it is <= the authoritative PB. It is built
	// from perLap, a pool that may not include the PB lap itself (unmatched or unanalyzable), so the
	// summed sector-bests can exceed the PB time — an impossible "prediction" faster in pieces than a
	// whole it never demonstrated together in a comparable way. When the PB lap's sectors aren't in the
	// pool, or the computed prediction would exceed PB, suppress it entirely rather than publish a
	// misleading number: we cannot produce a valid <= PB prediction without the PB lap's own sectors.
	_, pbAnalyzable := perLap[res.PBLap]
	switch {
	case res.PBLap == 0, !pbAnalyzable:
		// No PB-lap sectors to anchor the prediction to at all — can't produce a trustworthy <= PB
		// number, so suppress outright.
		res.PredictedBest, res.BestSectors, res.TimeOnTable = 0, nil, 0
	case res.PredictedBest > res.PBTime+processing.SectorJitterTol(res.PBTime):
		// Exceeds PB by more than trace-timestamp jitter can explain (see sectorAnalyzable) — the
		// prediction is untrustworthy, not just imprecise. Suppress rather than publish it.
		res.PredictedBest, res.BestSectors, res.TimeOnTable = 0, nil, 0
	case res.PredictedBest > res.PBTime:
		// Within the same wall-clock jitter band sectorAnalyzable already tolerates when admitting a
		// lap's sectors (a sum of real timestamps essentially never lands exactly on GT7's game-clock
		// duration, even for the very lap that produced it). Clamp to PB rather than suppress, so the
		// PredictedBest <= PBTime invariant holds exactly without discarding a trustworthy prediction.
		res.PredictedBest = res.PBTime
		if pbSectorSum > res.PredictedBest {
			res.TimeOnTable = pbSectorSum - res.PredictedBest
		} else {
			res.TimeOnTable = 0
		}
	}

	// Per-lap deltas relative to the authoritative PB; show the same duration-eligible + in-band laps
	// consistencyOf counts, so laps[] and consistency agree on which laps are "racing" laps.
	for _, l := range laps {
		if !l.Timed || !l.Complete || l.Fallback {
			continue
		}
		if l.Duration > gt7Best*3/2 {
			continue
		}
		res.Laps = append(res.Laps, LapAnalysis{
			LapNumber:    l.LapNumber,
			Duration:     l.Duration,
			DeltaToPB:    l.Duration - gt7Best,
			SectorDeltas: sectorDeltas(perLap[l.LapNumber], pbSectors),
		})
	}
	res.Recommendations = recommend(res)
	return res, nil
}

// matchLap returns the lap number whose duration equals gt7Best within a small tolerance (both are
// ms-precision GT7 times), or 0 when no captured lap matches (the fastest lap was not captured cleanly).
func matchLap(laps []processing.Lap, gt7Best time.Duration) int32 {
	const tol = 2 * time.Millisecond
	for _, l := range laps {
		if !l.Timed || !l.Complete || l.Fallback {
			continue
		}
		d := l.Duration - gt7Best
		if d < 0 {
			d = -d
		}
		if d <= tol {
			return l.LapNumber
		}
	}
	return 0
}

// recommend turns the deltas into short, data-supported statements (no invented advice — this is the
// deterministic layer; corner-level coaching comes when the trace supports it).
func recommend(r SessionAnalysis) []string {
	var pbLine string
	if r.PBLap > 0 {
		pbLine = fmt.Sprintf("Personal Best: lap %d at %s.", r.PBLap, formatDuration(r.PBTime))
	} else {
		pbLine = fmt.Sprintf("Personal Best: %s.", formatDuration(r.PBTime))
	}
	recs := []string{pbLine}

	if r.Consistency.Count >= 2 {
		recs = append(recs, fmt.Sprintf("Consistency: %d valid laps, spread %+.3fs (std-dev %.3fs).",
			r.Consistency.Count, r.Consistency.Spread.Seconds(), r.Consistency.StdDev.Seconds()))
	}

	// Improvement pointer from own data (no external reference needed).
	if r.PredictedBest > 0 && r.TimeOnTable > 0 {
		recs = append(recs, fmt.Sprintf(
			"Predicted best %s — %+.3fs on the table across your best sectors (each driven faster before, just not together).",
			formatDuration(r.PredictedBest), r.TimeOnTable.Seconds()))
	}

	// The lap furthest off PB, and where it lost the most.
	var worst LapAnalysis
	for _, l := range r.Laps {
		if l.DeltaToPB > worst.DeltaToPB {
			worst = l
		}
	}
	if worst.DeltaToPB > 0 {
		var ws SectorDelta
		haveSector := false
		for _, s := range worst.SectorDeltas {
			if !haveSector || s.DeltaToPB > ws.DeltaToPB {
				ws, haveSector = s, true
			}
		}
		if haveSector && ws.DeltaToPB > 0 {
			recs = append(recs, fmt.Sprintf("Lap %d was %+.3fs off PB; biggest loss in sector %d (%+.3fs).",
				worst.LapNumber, worst.DeltaToPB.Seconds(), ws.Index, ws.DeltaToPB.Seconds()))
		} else {
			recs = append(recs, fmt.Sprintf("Lap %d was %+.3fs off PB.", worst.LapNumber, worst.DeltaToPB.Seconds()))
		}
	}
	return recs
}

// formatDuration renders a lap time as M:SS.mmm.
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "-" + formatDuration(-d)
	}
	m := int(d / time.Minute)
	return fmt.Sprintf("%d:%06.3f", m, (d % time.Minute).Seconds())
}
