package processing

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
)

// Reference-lap sources (TrackReference.Source, and the API's "source" field).
const (
	// RefSourcePinned is a lap the user pinned as the track's reference.
	RefSourcePinned = "pinned"
	// RefSourceFastest is the fastest eligible stored lap on the track (no pin).
	RefSourceFastest = "fastest"
)

// ErrLapNotFound is returned by TrackRefStore.PinReference for a lap that is not stored.
var ErrLapNotFound = errors.New("processing: lap not found")

// TimeSpan is a half-open interval of lap start times [From, Until); a zero Until is open-ended.
type TimeSpan struct{ From, Until time.Time }

// TrackRefStore is the per-track side of the lap store: per-lap track labels, the stored laps of a
// track, and the user's pinned reference lap per track. MemLapStore and PostgresLapStore implement it.
type TrackRefStore interface {
	// LabelTrack marks the session's laps that started inside span ([From, Until); a zero From covers
	// from the session start, a zero Until is open-ended) and are unlabelled or provisional as
	// slug/confident: a detector lock backfilling its own detection stint. The current stint's backfill
	// is open-ended; a retry for an already-ended stint is bounded by that stint's end.
	LabelTrack(ctx context.Context, sessionID string, span TimeSpan, slug string) error
	// ClearProvisionalLabels nulls the track fields of the session's provisional laps: a lap whose
	// detector guess was never confirmed has no trustworthy track. Run once, when the session ends
	// (after any final lock backfill). Laps that started inside a keep span (a stint whose lock
	// backfill failed, so its labels are right but not promoted) are left alone. Confident and unlabelled laps, and other sessions, are untouched.
	ClearProvisionalLabels(ctx context.Context, sessionID string, keep ...TimeSpan) error
	// SetSessionTrack sets EVERY lap of the session to slug/confidence, overwriting any label (even a
	// confident detector lock): the user's track pick (confident) or its withdrawal (provisional, so
	// a later lock relabels the laps and an unconfirmed session end clears them).
	SetSessionTrack(ctx context.Context, sessionID, slug, confidence string) error
	// Lap returns one stored lap (found false when absent).
	Lap(sessionID string, lapNumber int32) (*Lap, bool, error)
	// TrackLaps returns up to limit reference-eligible laps on slug (see RefEligible), fastest first,
	// across every stored session and car.
	TrackLaps(slug string, limit int) ([]*Lap, error)
	// PinnedReference returns the lap pinned for slug (found false when none is pinned).
	PinnedReference(slug string) (*Lap, bool, error)
	// PinReference pins (or re-pins) a stored lap as slug's reference; ErrLapNotFound when absent.
	// Callers validate eligibility first (RefEligible). The pin goes when the lap's session does.
	PinReference(slug, sessionID string, lapNumber int32) error
	// UnpinReference removes slug's pin, reporting whether one existed.
	UnpinReference(slug string) (removed bool, err error)
	// UnpinReferenceIf removes slug's pin only if it still points at sessionID/lapNumber (a
	// compare-and-delete), reporting whether it did. Resolution uses it so a stale read of an ineligible
	// pin can never delete a newer pin set concurrently.
	UnpinReferenceIf(slug, sessionID string, lapNumber int32) (removed bool, err error)
}

// RefEligible reports whether l can be slug's reference lap: a complete, valid lap that the detector
// confidently placed on exactly slug (layout + direction), and not the first lap of a race. The
// session's total_laps rides on the Lap itself (Lap.TotalLaps, filled by every store), so this stays
// the single rule that the API, the live loader and the stores' queries all mirror.
func RefEligible(l *Lap, slug string) bool {
	return l.Complete && l.Valid && l.TrackSlug == slug && l.TrackConfidence == ConfidenceConfident && !l.RaceLap1()
}

// TrackReference is a resolved reference lap for a track and where it came from.
type TrackReference struct {
	Lap    Lap
	Source string // RefSourcePinned | RefSourceFastest
}

// ResolveTrackReference picks slug's reference lap: the pinned lap if any, otherwise the fastest
// reference-eligible stored lap. A pin that is no longer eligible is removed and logged (log may be nil).
// found is false when the track has neither.
func ResolveTrackReference(s TrackRefStore, slug string, log *zap.Logger) (TrackReference, bool, error) {
	l, ok, err := s.PinnedReference(slug)
	if err != nil {
		return TrackReference{}, false, err
	}
	if ok && RefEligible(l, slug) {
		return TrackReference{Lap: *l, Source: RefSourcePinned}, true, nil
	}
	if ok {
		// The pin is no longer valid (a race's lap 1 pinned before that rule existed, or while the
		// session's total_laps was not yet known). Remove it explicitly so the UI can show the fallback
		// honestly (its "Clear pin" only exists for a pinned source) and log it once.
		if removed, err := s.UnpinReferenceIf(slug, l.SessionID, l.LapNumber); err != nil {
			return TrackReference{}, false, err
		} else if removed && log != nil {
			log.Info("reference pin removed: lap not eligible", zap.String("slug", slug),
				zap.String("session", l.SessionID), zap.Int32("lap", l.LapNumber))
		}
	}
	laps, err := s.TrackLaps(slug, 1)
	if err != nil || len(laps) == 0 {
		return TrackReference{}, false, err
	}
	return TrackReference{Lap: *laps[0], Source: RefSourceFastest}, true, nil
}
