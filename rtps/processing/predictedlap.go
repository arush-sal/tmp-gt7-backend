package processing

import (
	"context"
	"fmt"
	"math"
	"sort"
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
	Steering      float32 // radians, mean of the front road-wheel angles (see Sample.Steering)
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

	// Pool: full racing laps (Timed && Complete && !Fallback) whose trace is sector-analyzable. A lap
	// with no trace / a read error is skipped rather than failing the whole request. Each lap keeps its
	// own per-lap distance fractions (Normalize) — the same axis analysis.predictedBest cuts sectors on,
	// so the predicted line and the analyzer's sector timing agree. The splice below emits each owner's
	// RAW positions for its sector span; a small position step at a sector seam (two laps crossing the
	// boundary at slightly different points) is accepted and far cheaper than projecting every lap onto
	// the RKB centerline (which mis-cut samples at a self-crossing like Suzuka's figure-8 and dropped
	// them from the splice).
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
		pool = append(pool, pooledLap{lapNumber: l.LapNumber, norm: norm, splits: SplitSectors(norm, sectors)})
	}
	// Ascending lap-number order so a per-sector duration tie resolves to the lowest lap number.
	sort.Slice(pool, func(i, j int) bool { return pool[i].lapNumber < pool[j].lapNumber })
	if len(pool) == 0 {
		return out, nil // empty pool -> no samples, zero predicted best
	}

	// Splice sectors in index order, stitching each owner's RAW samples. cumPredictedNs is the running
	// synthetic-lap time; distAcc is the spliced distance (Σ position deltas). At each owner change the
	// next owner starts at its sample nearest the previous owner's last position (sectorStartIndex — a
	// single-point match, not whole-lap projection), so the line is spatially continuous across the seam
	// even though two laps reach the boundary at slightly different per-lap distances. Emitted Fraction
	// and TNs are clamped non-decreasing so the small seam overlap can't break downstream monotonic
	// assumptions (PredictedElapsed's binary search).
	var cumPredictedNs, prevTNs int64
	var distAcc, prevFrac float64
	havePrev := false
	var prevX, prevZ float32
	for _, sec := range sectors {
		owner, ownerSplit, ok := ownerForSector(pool, sec.Index)
		if !ok {
			continue // no pooled owner (only reachable with an empty pool, handled above)
		}
		durNs := ownerSplit.Duration.Nanoseconds()
		startIdx := sectorStartIndex(owner.norm.Points, sec.Start, sec.End, havePrev, prevX, prevZ)
		if startIdx >= 0 {
			segStart := owner.norm.Points[startIdx].RecvTime // re-base sector time to the first emitted sample
			for i := startIdx; i < len(owner.norm.Points); i++ {
				p := owner.norm.Points[i]
				if p.Fraction > sec.End {
					break
				}
				if havePrev {
					distAcc += math.Hypot(float64(p.PosX-prevX), float64(p.PosZ-prevZ))
				}
				havePrev, prevX, prevZ = true, p.PosX, p.PosZ

				f := p.Fraction
				if f < prevFrac { // clamp non-decreasing across the seam overlap
					f = prevFrac
				}
				prevFrac = f
				tns := cumPredictedNs + p.RecvTime.Sub(segStart).Nanoseconds()
				if tns < prevTNs {
					tns = prevTNs
				}
				prevTNs = tns

				out.Samples = append(out.Samples, PredictedSample{
					Fraction:      f,
					TNs:           tns,
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
					Steering:      p.Steering,
					DeltaToBestNs: p.DeltaToBestNs,
				})
			}
		}
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

// seamSearchMargin bounds how far BEFORE a sector's Start (in per-lap distance fraction) the seam
// alignment looks for the owner's sample nearest the previous owner's last position. It must exceed the
// physical seam offset (two laps reach a boundary at slightly different distances — up to a few hundred
// metres) yet stay well within a sector so a self-crossing elsewhere on track can't be matched.
const seamSearchMargin = 0.05

// sectorStartIndex returns the index of the first owner sample to emit for a sector. For the first
// sector (no previous position) it is the first sample in [start, end]. Otherwise it is the sample in
// [start-seamSearchMargin, end] nearest the previous owner's last position (prevX, prevZ), so the
// spliced line picks up spatially where the previous owner left off (no seam teleport). Returns -1 when
// the owner has no sample in [start, end] (defensive — a chosen owner always does).
func sectorStartIndex(pts []NormSample, start, end float64, havePrev bool, prevX, prevZ float32) int {
	if !havePrev {
		for i := range pts {
			if pts[i].Fraction >= start && pts[i].Fraction <= end {
				return i
			}
		}
		return -1
	}
	best := math.Inf(1)
	idx, inRange := -1, false
	for i := range pts {
		f := pts[i].Fraction
		if f < start-seamSearchMargin || f > end {
			continue
		}
		if f >= start {
			inRange = true
		}
		if d := math.Hypot(float64(pts[i].PosX-prevX), float64(pts[i].PosZ-prevZ)); d < best {
			best, idx = d, i
		}
	}
	if !inRange {
		return -1
	}
	return idx
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
