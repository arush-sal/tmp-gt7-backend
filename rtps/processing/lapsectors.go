package processing

import (
	"context"
	"fmt"
	"time"

	"github.com/arush-sal/garage/backend/rtps/reference"
)

// SectorProvider resolves a session's track sectors, with a machine reason when real RKB geometry is
// unavailable. reason == "" => real RKB sectors returned in `sectors`; otherwise sectors is nil and
// the caller falls back to mini-sectors. reason ∈ {"track-not-detected","no-centerline","no-sector-labels"}.
type SectorProvider interface {
	SectorsForSession(ctx context.Context, sessionID string) (sectors []reference.Sector, reason string, err error)
}

// LapSectorSplit is one sector's timing for a lap, with its delta to the session's best for that sector.
type LapSectorSplit struct {
	Index       int
	Label       string // "S%d" of Index+1
	Duration    time.Duration
	DeltaToBest time.Duration // Duration - best[Index]; 0 when this lap owns the best; 0 when !HasDelta
	HasDelta    bool          // true only for a full racing lap with a comparable best; false otherwise
	MinSpeed    float32
}

// LapSectors is one lap's sector splits plus whether they are trustworthy geometry.
type LapSectors struct {
	LapNumber int32
	Reliable  bool   // real RKB sectors (reason=="") AND SectorAnalyzable(this lap)
	Reason    string // "" when Reliable; else the machine reason (rkb reason, or "trace-span-mismatch")
	Splits    []LapSectorSplit
}

// SessionLapSectors computes per-lap sector splits + per-sector delta-to-best for EVERY lap in `laps`
// (never drops a lap). It resolves the track's sectors ONCE via provider (all laps of a session share
// the same track). When provider returns reason != "", it falls back to MiniSectors(3) and every lap
// carries that reason with Reliable=false.
//
// best[i] = min Duration for sector i across only the FULL RACING laps that are also SectorAnalyzable.
// "Full racing" (Timed && Complete && !Fallback) is the same eligibility the analyzer uses: it excludes
// the formation/out lap and the partial final lap, whose traces cover only part of the track — their
// per-sector times would otherwise plant a bogus (far too fast) best and make every real lap's delta
// nonsensical. SectorAnalyzable additionally drops a spliced/teleport lap. DeltaToBest is emitted only
// for full racing laps (a partial lap's split spans only part of the track, so a delta against a
// full-lap best would mislead); other laps still carry their raw splits, just without a fabricated
// comparison. sessionID is used for provider + trace lookups. Returns entries in the same order as `laps`.
func SessionLapSectors(ctx context.Context, provider SectorProvider, traces TraceStore, sessionID string, laps []Lap) ([]LapSectors, error) {
	sectors, rkbReason, err := provider.SectorsForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if rkbReason != "" || sectors == nil {
		sectors = MiniSectors(3)
		if rkbReason == "" { // defensive: nil sectors with no reason -> treat as undetected track
			rkbReason = "track-not-detected"
		}
	}

	out := make([]LapSectors, len(laps))
	racing := make([]bool, len(laps)) // full racing lap: comparable full-track sector times
	// best[sector index] = min Duration among full-racing + analyzable laps; present only for such sectors.
	best := map[int]time.Duration{}

	for i, l := range laps {
		samples, err := traces.ByLap(sessionID, l.LapNumber)
		if err != nil {
			return nil, err
		}
		norm := Normalize(samples)
		splits := SplitSectors(norm, sectors)
		analyzable := SectorAnalyzable(l.Duration, samples)
		racing[i] = l.Timed && l.Complete && !l.Fallback
		inPool := racing[i] && analyzable // eligible AND timing-trustworthy -> may set the best

		ls := LapSectors{LapNumber: l.LapNumber, Splits: make([]LapSectorSplit, len(splits))}
		for j, s := range splits {
			ls.Splits[j] = LapSectorSplit{
				Index:    s.Index,
				Label:    fmt.Sprintf("S%d", s.Index+1),
				Duration: s.Duration,
				MinSpeed: s.MinSpeed,
			}
			if inPool {
				if b, seen := best[s.Index]; !seen || s.Duration < b {
					best[s.Index] = s.Duration
				}
			}
		}
		switch {
		case rkbReason != "":
			ls.Reason = rkbReason
		case !analyzable:
			ls.Reason = "trace-span-mismatch"
		default:
			ls.Reliable = true
		}
		out[i] = ls
	}

	// Second pass: emit delta-to-best only for full racing laps (see the doc comment on why partial /
	// formation laps get no delta), and only for sectors the racing pool actually populated.
	for i := range out {
		if !racing[i] {
			continue
		}
		for j := range out[i].Splits {
			if b, seen := best[out[i].Splits[j].Index]; seen {
				out[i].Splits[j].DeltaToBest = out[i].Splits[j].Duration - b
				out[i].Splits[j].HasDelta = true
			}
		}
	}
	return out, nil
}
