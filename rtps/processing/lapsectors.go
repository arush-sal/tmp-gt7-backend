package processing

import (
	"context"
	"fmt"
	"time"

	"github.com/arush-sal/garage/backend/rtps/reference"
)

// SectorProvider resolves track sectors, with a machine reason when real RKB geometry is unavailable.
// reason == "" => real RKB sectors returned in `sectors`; otherwise sectors is nil and the caller falls
// back to mini-sectors. reason ∈ {"track-not-detected","no-centerline","no-sector-labels"}.
type SectorProvider interface {
	// SectorsForSession resolves the session's live-detected track (laps stored without a track).
	SectorsForSession(ctx context.Context, sessionID string) (sectors []reference.Sector, reason string, err error)
	// SectorsForTrack resolves a track slug directly (a lap's own Lap.TrackSlug).
	SectorsForTrack(ctx context.Context, slug string) (sectors []reference.Sector, reason string, err error)
}

// lapSectorResolver resolves sectors per lap: the lap's own track when it has one (a session can drive
// several tracks), else the session's live track. Results are cached per track for one request, and
// every unusable result falls back to MiniSectors(3) with its reason.
type lapSectorResolver struct {
	ctx       context.Context
	provider  SectorProvider
	sessionID string
	cache     map[string]resolvedSectors
}

type resolvedSectors struct {
	sectors []reference.Sector
	reason  string
}

func newLapSectorResolver(ctx context.Context, provider SectorProvider, sessionID string) *lapSectorResolver {
	return &lapSectorResolver{ctx: ctx, provider: provider, sessionID: sessionID, cache: map[string]resolvedSectors{}}
}

// forLap returns the lap's sectors and reason ("" = real RKB sectors). Keyed by Lap.TrackSlug; "" is
// the session's live track.
func (r *lapSectorResolver) forLap(l Lap) ([]reference.Sector, string, error) {
	if c, ok := r.cache[l.TrackSlug]; ok {
		return c.sectors, c.reason, nil
	}
	var (
		sectors []reference.Sector
		reason  string
		err     error
	)
	if l.TrackSlug != "" {
		sectors, reason, err = r.provider.SectorsForTrack(r.ctx, l.TrackSlug)
	} else {
		sectors, reason, err = r.provider.SectorsForSession(r.ctx, r.sessionID)
	}
	if err != nil {
		return nil, "", err
	}
	sectors, reason = withFallback(sectors, reason)
	r.cache[l.TrackSlug] = resolvedSectors{sectors, reason}
	return sectors, reason, nil
}

// withFallback substitutes MiniSectors(3) for unusable RKB sectors, keeping (or defaulting) the reason.
func withFallback(sectors []reference.Sector, reason string) ([]reference.Sector, string) {
	if reason != "" || sectors == nil {
		if reason == "" { // defensive: nil sectors with no reason -> treat as undetected track
			reason = "track-not-detected"
		}
		return MiniSectors(3), reason
	}
	return sectors, ""
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
// (never drops a lap). Each lap is split on its OWN track's sectors (lapSectorResolver: Lap.TrackSlug,
// else the session's live track), and the per-sector best is pooled per track, since one session can
// drive several tracks. Where provider returns reason != "", that track falls back to MiniSectors(3)
// and its laps carry the reason with Reliable=false.
//
// best[i] = min Duration for sector i across only the FULL RACING laps on the same track that are also
// SectorAnalyzable.
// "Full racing" (Timed && Complete && !Fallback) is the same eligibility the analyzer uses: it excludes
// the formation/out lap and the partial final lap, whose traces cover only part of the track — their
// per-sector times would otherwise plant a bogus (far too fast) best and make every real lap's delta
// nonsensical. SectorAnalyzable additionally drops a spliced/teleport lap. DeltaToBest is emitted only
// for full racing laps (a partial lap's split spans only part of the track, so a delta against a
// full-lap best would mislead); other laps still carry their raw splits, just without a fabricated
// comparison. sessionID is used for provider + trace lookups. Returns entries in the same order as `laps`.
func SessionLapSectors(ctx context.Context, provider SectorProvider, traces TraceStore, sessionID string, laps []Lap) ([]LapSectors, error) {
	resolver := newLapSectorResolver(ctx, provider, sessionID)
	out := make([]LapSectors, len(laps))
	racing := make([]bool, len(laps)) // full racing lap: comparable full-track sector times
	// bestBy[track][sector index] = min Duration among that track's full-racing + analyzable laps;
	// present only for such sectors.
	bestBy := map[string]map[int]time.Duration{}

	for i, l := range laps {
		sectors, rkbReason, err := resolver.forLap(l)
		if err != nil {
			return nil, err
		}
		best := bestBy[l.TrackSlug]
		if best == nil {
			best = map[int]time.Duration{}
			bestBy[l.TrackSlug] = best
		}
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
			// Only a clean (never all-four-off-track) sector may set the best; an off-track sector must
			// not count toward the fastest-sector time. A lap's own dirty sector still reports its
			// duration + delta vs the clean best (second pass below).
			if inPool && s.Clean {
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
		best := bestBy[laps[i].TrackSlug]
		for j := range out[i].Splits {
			if b, seen := best[out[i].Splits[j].Index]; seen {
				out[i].Splits[j].DeltaToBest = out[i].Splits[j].Duration - b
				out[i].Splits[j].HasDelta = true
			}
		}
	}
	return out, nil
}
