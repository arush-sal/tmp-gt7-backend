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
	// Improvement view: the theoretical-best lap (sum of the fastest sector times the driver has
	// achieved) and how much time that leaves on the table vs the PB's sectors.
	TheoreticalBest time.Duration `json:"theoretical_best_ns"`
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

// AnalyzeSession computes the analysis for one session: pick the PB, then for each valid lap compute
// its delta to PB and per-sector deltas (via the distance-normalized trace + RKB sectors), plus
// consistency and recommendations.
func (a Analyzer) AnalyzeSession(ctx context.Context, sessionID string) (SessionAnalysis, error) {
	lapPtrs := a.Laps.BySession(sessionID)
	laps := make([]processing.Lap, 0, len(lapPtrs))
	for _, p := range lapPtrs {
		if p == nil { // defensive: stores return non-nil, but don't panic if that ever changes
			continue
		}
		laps = append(laps, *p)
	}
	res := SessionAnalysis{SessionID: sessionID, Consistency: consistencyOf(laps)}

	pb, ok := PersonalBest(laps)
	if !ok {
		res.Recommendations = []string{"No valid (complete, timed, gap-free) lap in this session — no Personal Best to analyze."}
		return res, nil
	}
	res.HasPB, res.PBLap, res.PBTime = true, pb.LapNumber, pb.Duration

	// Compute each valid lap's sectors once; reuse for deltas + the theoretical best.
	perLap := make(map[int32][]processing.SectorResult)
	for _, l := range laps {
		if !l.Valid {
			continue
		}
		secs, err := a.sectorsOf(ctx, sessionID, l.LapNumber)
		if err != nil {
			return res, err
		}
		perLap[l.LapNumber] = secs
	}
	pbSectors := perLap[pb.LapNumber]

	// Improvement view: theoretical best = sum of the fastest sector times; time on the table = how
	// much the PB's sectors trail those bests (both sector-time based, so apples-to-apples).
	res.TheoreticalBest, res.BestSectors = theoreticalBest(perLap)
	var pbSectorSum time.Duration
	for _, sd := range pbSectors {
		pbSectorSum += sd.Duration
	}
	if pbSectorSum > res.TheoreticalBest {
		res.TimeOnTable = pbSectorSum - res.TheoreticalBest
	}

	for _, l := range laps {
		if !l.Valid {
			continue
		}
		res.Laps = append(res.Laps, LapAnalysis{
			LapNumber:    l.LapNumber,
			Duration:     l.Duration,
			DeltaToPB:    l.Duration - pb.Duration,
			SectorDeltas: sectorDeltas(perLap[l.LapNumber], pbSectors),
		})
	}
	res.Recommendations = recommend(res)
	return res, nil
}

// sectorsOf resolves one lap's per-sector metrics: load its trace, distance-normalize it, and split
// by the RKB's sectors (falling back to mini-sectors when the track is unknown).
func (a Analyzer) sectorsOf(ctx context.Context, sessionID string, lapNumber int32) ([]processing.SectorResult, error) {
	samples, err := a.Traces.ByLap(sessionID, lapNumber)
	if err != nil {
		return nil, err
	}
	norm := processing.Normalize(samples)
	secs, _, err := processing.SectorsForLap(ctx, a.KB, sessionID, norm, a.FallbackSectors)
	return secs, err
}

// recommend turns the deltas into short, data-supported statements (no invented advice — this is the
// deterministic layer; corner-level coaching comes when the trace supports it).
func recommend(r SessionAnalysis) []string {
	recs := []string{fmt.Sprintf("Personal Best: lap %d at %s.", r.PBLap, formatDuration(r.PBTime))}

	if r.Consistency.Count >= 2 {
		recs = append(recs, fmt.Sprintf("Consistency: %d valid laps, spread %+.3fs (std-dev %.3fs).",
			r.Consistency.Count, r.Consistency.Spread.Seconds(), r.Consistency.StdDev.Seconds()))
	}

	// Improvement pointer from own data (no external reference needed).
	if r.TheoreticalBest > 0 && r.TimeOnTable > 0 {
		recs = append(recs, fmt.Sprintf(
			"Theoretical best %s — %+.3fs on the table across your best sectors (each driven faster before, just not together).",
			formatDuration(r.TheoreticalBest), r.TimeOnTable.Seconds()))
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
