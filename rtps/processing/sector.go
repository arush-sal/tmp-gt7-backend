package processing

import (
	"context"
	"math"
	"time"

	"github.com/arush-sal/garage/backend/rtps/reference"
)

// SectorResult is one sector's derived metrics for a lap: how long it took and the minimum speed
// through it (a min-corner-speed proxy). Start/End are the normalized-distance bounds it was cut at.
type SectorResult struct {
	Index    int
	Start    float64
	End      float64
	Duration time.Duration
	MinSpeed float32 // km/h; minimum speed among samples in [Start, End]
	// Clean is false when the car went off-track (all four tyres off) at any sample in the sector, so
	// this sector must not count toward the fastest-sector selection. True when no off-track sample was
	// seen (including when surface data is absent — missing data never invalidates).
	Clean bool
}

// SplitSectors cuts a distance-normalized lap into per-sector metrics at the given sector boundaries.
// Sector duration is the interpolated elapsed time between its Start and End fractions; MinSpeed is
// the slowest sample within the sector. Boundaries are taken as-is (the RKB owns their correctness);
// an empty lap yields zero-value results.
func SplitSectors(n NormalizedLap, sectors []reference.Sector) []SectorResult {
	out := make([]SectorResult, 0, len(sectors))
	for _, s := range sectors {
		r := SectorResult{Index: s.Index, Start: s.Start, End: s.End, MinSpeed: n.minSpeed(s.Start, s.End), Clean: n.sectorClean(s.Start, s.End)}
		start, ok1 := n.TimeAt(s.Start)
		end, ok2 := n.TimeAt(s.End)
		if ok1 && ok2 {
			r.Duration = end.Sub(start)
		}
		out = append(out, r)
	}
	return out
}

// offTrackSurfaces are the GT7 per-wheel surface codes that count as off-track (dirt, grass, sand,
// snow). Tarmac (T) and kerb (C) are on-track — kerbs are within track limits, so riding them is legal.
// Reverse-engineered code table, see collector/protocol/constants.go:offSurfaceType.
const offTrackSurfaces = "DGSs"

// allTyresOff reports whether all four wheels are off-track for one sample. surface is GT7's 4-char
// per-wheel code (FL,FR,RL,RR). It is true ONLY when the string is exactly 4 chars and every wheel is
// on an off-track surface — so empty/short (format-A, missing data) and any unknown code yield false,
// i.e. missing/ambiguous data never invalidates a sector.
func allTyresOff(surface string) bool {
	if len(surface) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		off := false
		for j := 0; j < len(offTrackSurfaces); j++ {
			if surface[i] == offTrackSurfaces[j] {
				off = true
				break
			}
		}
		if !off {
			return false // this wheel is on track (or unknown) -> not all-four-off
		}
	}
	return true
}

// sectorClean reports whether the car stayed on track (never all four tyres off) across every sample in
// the sector's [start, end] fraction range. A sector with no samples in range is clean by default.
func (n NormalizedLap) sectorClean(start, end float64) bool {
	for _, p := range n.Points {
		if p.Fraction >= start && p.Fraction <= end && allTyresOff(p.SurfaceType) {
			return false
		}
	}
	return true
}

// minSpeed returns the slowest speed among samples whose fraction is within [start, end]. If no sample
// falls in the range (a very short sector between two knots), it interpolates at the midpoint.
func (n NormalizedLap) minSpeed(start, end float64) float32 {
	min := float32(math.MaxFloat32)
	found := false
	for _, p := range n.Points {
		if p.Fraction >= start && p.Fraction <= end {
			if !found || p.Speed < min {
				min, found = p.Speed, true
			}
		}
	}
	if !found {
		if s, ok := n.At((start + end) / 2); ok {
			return s.Speed
		}
		return 0
	}
	return min
}

// MiniSectors returns count equal-distance sectors spanning 0..1 — the track-agnostic fallback used
// when the RKB can't identify the track (D-B16). count <= 0 yields a single whole-lap sector.
func MiniSectors(count int) []reference.Sector {
	if count <= 0 {
		count = 1
	}
	secs := make([]reference.Sector, count)
	for i := range secs {
		secs[i] = reference.Sector{
			Index: i,
			Start: float64(i) / float64(count),
			End:   float64(i+1) / float64(count),
		}
	}
	return secs
}

// SectorsForLap resolves a session's track via the RKB and splits the lap into that track's sectors.
// When the track is unknown (or has no sector definition), it falls back to fallbackCount equal
// mini-sectors. usedRKB reports whether real RKB sectors were used (vs the fallback).
func SectorsForLap(ctx context.Context, kb reference.KB, sessionID string, n NormalizedLap, fallbackCount int) (results []SectorResult, usedRKB bool, err error) {
	track, found, err := kb.TrackForSession(ctx, sessionID)
	if err != nil {
		return nil, false, err
	}
	if found {
		sectors, err := kb.Sectors(ctx, track.ID)
		if err != nil {
			return nil, false, err
		}
		if len(sectors) > 0 {
			return SplitSectors(n, sectors), true, nil
		}
	}
	return SplitSectors(n, MiniSectors(fallbackCount)), false, nil
}
