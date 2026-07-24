package processing

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/arush-sal/garage/backend/rtps/geom"
)

// RefSectorSeg records which pooled lap owns one sector of the predicted lap and how long that owner's
// sector took (nanoseconds). Label is "S%d" of Index+1 (matching LapSectorSplit.Label).
type RefSectorSeg struct {
	Index      int
	Label      string
	OwnerLap   int32
	DurationNs int64
}

// PredictedSample is one re-timed, re-distanced telemetry point of the spliced predicted lap. Fraction
// is the owner sample's original track-distance fraction; TNs is elapsed time in the SYNTHETIC lap (the
// owner's in-sector elapsed time, offset by the predicted duration of the sectors before it); Distance
// is re-accumulated across the whole splice (Σ position deltas) because segments come from different
// laps, so the owner samples' raw Distance is not comparable across the seams.
type PredictedSample struct {
	Fraction      float64
	TNs           int64
	Distance      float64
	PosX          float32
	PosZ          float32
	Speed         float32
	Throttle      float32
	Brake         float32
	RPM           float32
	Gear          int32
	Fuel          float32
	TyreTempFL    float32
	TyreTempFR    float32
	TyreTempRL    float32
	TyreTempRR    float32
	Clutch        float32
	Boost         float32
	DeltaToBestNs *int64
}

// PredictedLap is the demonstrated-ideal lap: the fastest sector segments the driver has actually run,
// spliced into one synthetic lap's full telemetry trace. Reliable is true only when real RKB geometry
// (reason=="") was used; on the mini-sector fallback it is false and Reason carries the machine reason.
// PredictedBestNs is the sum of the owner sectors' durations (<= any single actual lap). An empty pool
// yields empty Samples and a zero PredictedBestNs.
type PredictedLap struct {
	Reliable        bool
	Reason          string
	PredictedBestNs int64
	Sectors         []RefSectorSeg
	Samples         []PredictedSample
}

// pooledLap is one lap eligible to own sectors: its normalized trace and its per-sector split timings.
type pooledLap struct {
	lapNumber int32
	norm      NormalizedLap
	splits    []SectorResult
}

// PredictedReferenceLap builds the predicted (ideal) lap for a session: it resolves the track's sectors
// once, pools the session's full racing laps whose traces faithfully cover them (same gate as
// SessionLapSectors' best pool + SectorAnalyzable), picks the fastest owner per sector, and splices the
// owners' in-sector samples into one synthetic lap — re-timing each sector to start at the running
// predicted time and re-accumulating distance across the seams. Owner ties resolve to the lowest lap
// number (mirrors analysis.predictedBest). An empty pool returns an empty (non-error) PredictedLap.
func PredictedReferenceLap(ctx context.Context, provider SectorProvider, traces TraceStore, sessionID string, laps []Lap) (PredictedLap, error) {
	sectors, reason, err := provider.SectorsForSession(ctx, sessionID)
	if err != nil {
		return PredictedLap{}, err
	}
	if reason != "" || sectors == nil {
		sectors = MiniSectors(3)
		if reason == "" { // defensive: nil sectors with no reason -> treat as undetected track
			reason = "track-not-detected"
		}
	}
	out := PredictedLap{Reliable: reason == "", Reason: reason}

	// Resolve the session's centerline once. Sector boundaries are fractions on the centerline
	// arc-length axis, but Normalize sets each sample's Fraction on the lap's own racing-line distance
	// axis (differs per lap). When a usable centerline exists, re-fraction each pooled lap onto the
	// shared centerline-arc axis so a sector's canonical Start/End cuts every owner at the SAME physical
	// point (no position discontinuity at a splice seam). No usable centerline -> keep Normalize's
	// per-lap fractions (unchanged fallback output). A centerline lookup error is non-fatal here (the
	// same store was already read by SectorsForSession above) and falls back.
	var cl [][2]float64
	var arc []float64
	var totalArc float64
	if clRaw, _, cErr := provider.CenterlineForSession(ctx, sessionID); cErr == nil && len(clRaw) >= 2 {
		if a := geom.ClArcLengths(clRaw); a[len(a)-1] > 0 {
			cl, arc, totalArc = clRaw, a, a[len(a)-1]
		}
	}

	// Pool: full racing laps (Timed && Complete && !Fallback) whose trace is sector-analyzable. A lap
	// with no trace / a read error is skipped rather than failing the whole request.
	var pool []pooledLap
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
		if totalArc > 0 {
			refractionOnCenterline(norm, cl, arc, totalArc)
		}
		pool = append(pool, pooledLap{lapNumber: l.LapNumber, norm: norm, splits: SplitSectors(norm, sectors)})
	}
	// Ascending lap-number order so a per-sector duration tie resolves to the lowest lap number.
	sort.Slice(pool, func(i, j int) bool { return pool[i].lapNumber < pool[j].lapNumber })
	if len(pool) == 0 {
		return out, nil // empty pool -> no samples, zero predicted best
	}

	// Splice sectors in index order. cumPredictedNs is the synthetic-lap time at the current sector's
	// start; distAcc is the running spliced distance (Σ hypot of position deltas across all segments).
	var cumPredictedNs int64
	var distAcc float64
	havePrev := false
	var prevX, prevZ float32
	for _, sec := range sectors {
		owner, ownerSplit, ok := ownerForSector(pool, sec.Index)
		if !ok {
			continue // no pooled owner (only reachable with an empty pool, handled above)
		}
		ownerStart, ok := owner.norm.TimeAt(sec.Start)
		if !ok {
			continue
		}
		for _, p := range owner.norm.Points {
			if p.Fraction < sec.Start || p.Fraction > sec.End {
				continue
			}
			if havePrev {
				distAcc += math.Hypot(float64(p.PosX-prevX), float64(p.PosZ-prevZ))
			}
			havePrev, prevX, prevZ = true, p.PosX, p.PosZ

			out.Samples = append(out.Samples, PredictedSample{
				Fraction:      p.Fraction,
				TNs:           cumPredictedNs + p.RecvTime.Sub(ownerStart).Nanoseconds(),
				Distance:      distAcc,
				PosX:          p.PosX,
				PosZ:          p.PosZ,
				Speed:         p.Speed,
				Throttle:      p.Throttle,
				Brake:         p.Brake,
				RPM:           p.RPM,
				Gear:          p.Gear,
				Fuel:          p.Fuel,
				TyreTempFL:    p.TyreTempFL,
				TyreTempFR:    p.TyreTempFR,
				TyreTempRL:    p.TyreTempRL,
				TyreTempRR:    p.TyreTempRR,
				Clutch:        p.Clutch,
				Boost:         p.Boost,
				DeltaToBestNs: p.DeltaToBestNs,
			})
		}
		durNs := ownerSplit.Duration.Nanoseconds()
		cumPredictedNs += durNs
		out.PredictedBestNs += durNs
		out.Sectors = append(out.Sectors, RefSectorSeg{
			Index:      sec.Index,
			Label:      fmt.Sprintf("S%d", sec.Index+1),
			OwnerLap:   owner.lapNumber,
			DurationNs: durNs,
		})
	}
	return out, nil
}

// refractionJumpTol is the largest plausible forward jump in normalized centerline arc between two
// consecutive (60Hz) trace samples. A real car covers a tiny fraction of the lap per frame (even 300
// km/h over a 5 km lap is ~0.02%/frame; a multi-second frame gap is still a few percent), so any single
// step beyond this is a projection artefact — the global-nearest ProjectToClArc snapping to a FAR
// centerline segment where the track passes near itself (e.g. Suzuka's figure-8 crossover) or across
// the start/finish seam — not real progress.
const refractionJumpTol = 0.2

// refractionOnCenterline rewrites each point's Fraction as its centerline-arc projection normalized by
// totalArc, replacing Normalize's per-lap distance fraction. Fractions are clamped to [0,1] and forced
// monotonically non-decreasing (max with the previous) so NormalizedLap.locate's binary search stays
// valid. A lap starts at the start/finish line, so prev seeds at 0; a projection that jumps FORWARD by
// more than refractionJumpTol is a self-crossing/seam mis-projection and is rejected (the point holds
// the previous arc) — without this, one bad forward snap would pin every later sample high via the
// monotonic clamp and collapse the whole lap onto a tiny arc range. Small backward wander (racing line
// drift) is clamped up to prev as normal. Mutates n.Points in place. arc must be geom.ClArcLengths(cl);
// caller guarantees len(cl) >= 2 and totalArc > 0.
func refractionOnCenterline(n NormalizedLap, cl [][2]float64, arc []float64, totalArc float64) {
	prev := 0.0
	for i := range n.Points {
		a, ok := geom.ProjectToClArc(cl, arc, float64(n.Points[i].PosX), float64(n.Points[i].PosZ))
		if !ok {
			n.Points[i].Fraction = prev // no projection -> hold position, keep fractions monotonic
			continue
		}
		f := a / totalArc
		if f < 0 {
			f = 0
		} else if f > 1 {
			f = 1
		}
		if f > prev+refractionJumpTol {
			n.Points[i].Fraction = prev // implausible forward teleport (crossover/seam) -> hold prev
			continue
		}
		if f < prev {
			f = prev // small backward wander -> clamp monotonic
		}
		prev = f
		n.Points[i].Fraction = f
	}
}

// ownerForSector returns the pooled lap with the minimum sector-`index` duration and that lap's split
// for the sector. pool must already be in ascending lap-number order so a tie (equal durations) resolves
// to the lowest lap number (the strict `<` keeps the first, lowest lap). ok is false only when no pooled
// lap has a split for the sector.
//
// Off-track sectors (Clean == false, all four tyres off track somewhere in the sector) are NOT eligible
// to own a sector — that is the whole point of the invalidation. But if EVERY pooled lap ran this sector
// dirty, we fall back to the min-duration dirty split rather than drop the sector entirely, so the
// predicted lap stays whole (documented last resort).
func ownerForSector(pool []pooledLap, index int) (pooledLap, SectorResult, bool) {
	var cleanLap, anyLap pooledLap
	var cleanSplit, anySplit SectorResult
	cleanFound, anyFound := false, false
	for _, pl := range pool {
		for _, sp := range pl.splits {
			if sp.Index != index {
				continue
			}
			// A zero/negative duration means this lap did not actually drive the sector (its trace does
			// not cover the range, so TimeAt(start)==TimeAt(end)). Such a split is vacuously "clean" (no
			// samples in range) but must never own a sector — skip it, mirroring analysis.predictedBest.
			if sp.Duration <= 0 {
				break
			}
			if !anyFound || sp.Duration < anySplit.Duration {
				anyLap, anySplit, anyFound = pl, sp, true
			}
			if sp.Clean && (!cleanFound || sp.Duration < cleanSplit.Duration) {
				cleanLap, cleanSplit, cleanFound = pl, sp, true
			}
			break
		}
	}
	if cleanFound {
		return cleanLap, cleanSplit, true
	}
	return anyLap, anySplit, anyFound // fallback: all laps ran this sector off-track (or none drove it)
}
