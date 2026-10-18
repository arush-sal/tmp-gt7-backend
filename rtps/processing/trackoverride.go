package processing

import (
	"slices"
	"sync"
)

// TrackOverride is the user's "this is <track>" pick. HTTP handlers call Set/Clear; a pick for an open
// session is committed to the TrackDetector's session state synchronously (under mu, which the
// detector's session start and end also take), so when Set answers "current" the pick IS the session's:
// Track returns it, it is published, and a session end that follows keeps it on the session's laps.
// Only the stored-lap relabel (I/O) is left to the ingest goroutine: the session's next frame, or its
// end, whichever comes first (TrackDetector.syncUser). A pick made with no session open waits for the
// next session start.
//
// Several sessions can be open at once (one per gRPC stream): "current" is the most recently started
// one still open. Lock order: mu, then the detector's mu, then a session's mu.
type TrackOverride struct {
	d *TrackDetector

	mu   sync.Mutex
	next string   // pick waiting for the next session ("" = none)
	open []string // sessions open on the detector, in start order (the last is "current")
	user string   // the user's track last published by the detector ("" = none)
}

// Values of TrackOverride.Set's appliesTo.
const (
	AppliesToCurrent = "current"
	AppliesToNext    = "next"
)

// NewTrackOverride returns an override with no pick, attached to d (d.Override).
func NewTrackOverride(d *TrackDetector) *TrackOverride {
	o := &TrackOverride{d: d}
	d.Override = o
	return o
}

// Set picks slug (the caller validated it against the RKB). With a session open it is committed to the
// most recently started open session before returning (appliesTo "current", sessionID its id),
// replacing any earlier pick; otherwise it waits for the next session (appliesTo "next", sessionID ""),
// replacing any earlier pending pick.
func (o *TrackOverride) Set(slug string) (appliesTo, sessionID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.open) == 0 {
		o.next = slug
		return AppliesToNext, ""
	}
	id := o.open[len(o.open)-1]
	o.commit(id, slug)
	return AppliesToCurrent, id
}

// Clear withdraws the pick: a pending one is dropped; the current session's (if any) is withdrawn at
// once and detection resumes. A no-op when there is none.
func (o *TrackOverride) Clear() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.next = ""
	if len(o.open) > 0 {
		o.commit(o.open[len(o.open)-1], "")
	}
}

// Pending returns the pick waiting for the next session ("" = none).
func (o *TrackOverride) Pending() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.next
}

// UserTrack returns the track the detector currently publishes because the user picked it ("" when
// the published track, if any, is detected). It persists past the session end, like the published
// track, until the next session starts.
func (o *TrackOverride) UserTrack() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.user
}

// commit applies slug (or its withdrawal, "") to the open session id. Caller holds o.mu.
func (o *TrackOverride) commit(id, slug string) {
	if o.d.commitUser(id, slug) {
		o.user = slug
	}
}

// start marks sessionID open and commits the pending pick (if any) to it. Ingest side, after the
// detector created the session's state.
func (o *TrackOverride) start(sessionID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.open = append(o.open, sessionID)
	o.user = ""
	if o.next != "" {
		o.commit(sessionID, o.next)
		o.next = ""
	}
}

// end marks sessionID closed: later picks go to another open session or the next one. Ingest side,
// before the detector drops the session's state, so a pick is either committed before the end (and
// kept) or made after it.
func (o *TrackOverride) end(sessionID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.open = slices.DeleteFunc(o.open, func(id string) bool { return id == sessionID })
}
