package session

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Errors returned by RecordingControl.Set.
var (
	// ErrNoSession means no session is open on the stream (menus/idle): there is nothing to switch.
	ErrNoSession = errors.New("no active session")
	// ErrSessionChanged means the session the caller targeted is no longer the open one (it ended, or
	// another session opened before the request was applied). Nothing was changed.
	ErrSessionChanged = errors.New("session changed")
	// ErrStalled means the Detector did not pick the request up in time (telemetry stalled). The
	// request was withdrawn, so it can never be applied later to some other session.
	ErrStalled = errors.New("telemetry stalled; recording unchanged")
)

// recRequest is one queued Set. The Detector fills err and closes done when it takes or drops it.
type recRequest struct {
	on     bool
	expect string // session id the caller targets ("" = whatever is open); only checked for off
	done   chan struct{}
	err    error
}

// RecordingControl is the user's Record switch for one telemetry stream (the in-process source). It is
// the only cross-goroutine handle on a Detector: the HTTP handler queues a request with Set, and the
// Detector (on the ingest goroutine) applies it at its next Observe and publishes the resulting state
// back. Detector state is never touched from the caller's goroutine.
//
// Recording is ON by default and resets to ON whenever a session ends. Turning it OFF discards the
// open session entirely (DiscardRecordingOff) and swallows the rest of that stint; turning it back ON
// while still on track starts a fresh session from that frame.
type RecordingControl struct {
	pending atomic.Bool // a request is queued; checked lock-free on every frame (hot path)

	mu        sync.Mutex
	req       *recRequest
	recording bool   // published by the Detector
	active    bool   // a session (pending or confirmed) is open on the stream
	sessionID string // the open RECORDED session's id; "" when idle or not recording
	curID     string // the open session's id, recorded or not; "" when idle
	dupOf     string // the stored original the open session duplicates; "" when none
}

// NewRecordingControl returns a control in its default state: recording, no open session.
func NewRecordingControl() *RecordingControl { return &RecordingControl{recording: true} }

// Recording reports whether the stream is recording (true when idle).
func (c *RecordingControl) Recording() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recording
}

// State returns the published recording flag and the open recorded session's id ("" when none).
func (c *RecordingControl) State() (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recording, c.sessionID
}

// matches reports whether expect names the open session: its own id, or the stored original it
// duplicates (the live view shows the original while a replay of it plays). Caller holds mu.
func (c *RecordingControl) matches(expect string) bool {
	return expect == "" || expect == c.curID || (c.dupOf != "" && expect == c.dupOf)
}

// Set requests recording on or off and waits up to wait for the Detector to apply it, then returns the
// resulting state: recording and the open recorded session's id ("" after turning off; the fresh
// session's id after turning on, once its first on-track frame arrived). expect, when non-empty, is the
// session the caller saw; turning off applies only while that is still the open session (or the
// original it duplicates), checked here and again on the ingest goroutine. Errors: ErrNoSession (idle),
// ErrSessionChanged (expect no longer open, or the session ended before the request applied),
// ErrStalled (not applied within wait; the request is withdrawn). Setting the current value is an
// immediate no-op.
func (c *RecordingControl) Set(on bool, expect string, wait time.Duration) (recording bool, sessionID string, err error) {
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return c.Recording(), "", ErrNoSession
	}
	if !on && !c.matches(expect) {
		c.mu.Unlock()
		return c.Recording(), "", ErrSessionChanged
	}
	if c.recording == on && c.req == nil {
		defer c.mu.Unlock()
		return c.recording, c.sessionID, nil
	}
	if c.req != nil { // a newer request supersedes a queued one
		c.req.err = ErrSessionChanged
		close(c.req.done)
	}
	req := &recRequest{on: on, expect: expect, done: make(chan struct{})}
	c.req = req
	c.pending.Store(true)
	c.mu.Unlock()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-req.done:
	case <-timer.C:
		c.mu.Lock()
		if c.req == req { // still queued: withdraw it so it can never apply to a later session
			c.req = nil
			c.pending.Store(false)
			c.mu.Unlock()
			r, _ := c.State()
			return r, "", ErrStalled
		}
		c.mu.Unlock()
		<-req.done // taken or superseded meanwhile; its outcome is imminent
	}
	r, id := c.State()
	return r, id, req.err
}

// take dequeues the pending request (Detector side); nil when none is queued.
func (c *RecordingControl) take() *recRequest {
	if !c.pending.Load() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.req
	c.req = nil
	c.pending.Store(false)
	return r
}

// dropPending resolves a still-queued request when the session ends before it could apply (Detector
// side, from finalize). An off request reports ErrSessionChanged; an on request is satisfied, since
// recording resets to on at every session end.
func (c *RecordingControl) dropPending() {
	if !c.pending.Load() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := c.req; r != nil {
		if !r.on {
			r.err = ErrSessionChanged
		}
		close(r.done)
		c.req = nil
	}
	c.pending.Store(false)
}

// publish records the Detector's current state (Detector side; called per session event, not per frame).
func (c *RecordingControl) publish(recording, active bool, sessionID, curID, dupOf string) {
	c.mu.Lock()
	c.recording, c.active, c.sessionID, c.curID, c.dupOf = recording, active, sessionID, curID, dupOf
	c.mu.Unlock()
}
