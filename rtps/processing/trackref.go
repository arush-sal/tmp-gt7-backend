package processing

import (
	"errors"
	"time"
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

// TrackRefStore is the per-track side of the lap store: per-lap track labels, the stored laps of a
// track, and the user's pinned reference lap per track. MemLapStore and PostgresLapStore implement it.
type TrackRefStore interface {
	// LabelTrack marks the session's laps that started at or after since and are unlabelled or
	// provisional as slug/confident: a detector lock backfilling its own detection stint. A zero since
	// covers the whole session (the first stint).
	LabelTrack(sessionID string, since time.Time, slug string) error
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
}

// RefEligible reports whether l can be slug's reference lap: a complete, valid lap that the detector
// confidently placed on exactly slug (layout + direction).
func RefEligible(l *Lap, slug string) bool {
	return l.Complete && l.Valid && l.TrackSlug == slug && l.TrackConfidence == ConfidenceConfident
}

// TrackReference is a resolved reference lap for a track and where it came from.
type TrackReference struct {
	Lap    Lap
	Source string // RefSourcePinned | RefSourceFastest
}

// ResolveTrackReference picks slug's reference lap: the pinned lap if any, otherwise the fastest
// reference-eligible stored lap. found is false when the track has neither.
func ResolveTrackReference(s TrackRefStore, slug string) (TrackReference, bool, error) {
	if l, ok, err := s.PinnedReference(slug); err != nil || ok {
		if !ok {
			return TrackReference{}, false, err
		}
		return TrackReference{Lap: *l, Source: RefSourcePinned}, true, nil
	}
	laps, err := s.TrackLaps(slug, 1)
	if err != nil || len(laps) == 0 {
		return TrackReference{}, false, err
	}
	return TrackReference{Lap: *laps[0], Source: RefSourceFastest}, true, nil
}
