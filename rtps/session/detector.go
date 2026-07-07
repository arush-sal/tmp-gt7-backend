package session

import (
	"fmt"
	"strconv"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
)

// Default detector tuning, derived empirically from the 4-race capture (D-B4). Both windows are ~0.5s:
// long enough to absorb the ~0.1s finish-line results flap, short enough not to merge distinct stints.
const (
	defaultDebounce   = 500 * time.Millisecond
	defaultMinSession = 500 * time.Millisecond
	// defaultResetThreshold is deliberately large: package_id was strictly monotonic across the whole
	// capture (D-B2), so a reset is only a defensive safety-net for replay/reconnect. Only a backward
	// jump larger than this counts as a reset, never normal jitter.
	defaultResetThreshold = 1000
)

// Config tunes the Detector. Zero-value fields take the defaults above, so Config{} is valid.
type Config struct {
	Debounce                time.Duration // how long in_race must hold an edge before it takes effect
	MinSession              time.Duration // a session shorter than this at close is dropped as noise
	PackageIDResetThreshold uint32        // backward package_id jump beyond this = defensive reset
	NewID                   func() string // session id generator; injected for deterministic tests
	// OnError, if set, is called when persisting a session fails. The Sink/Observe API is error-less,
	// so this surfaces store failures (e.g. DB down) that would otherwise be silent. nil = ignore
	// (fine for MemStore, which never errors).
	OnError func(error)
}

// Detector is the Session Management state machine. It consumes one telemetry stream's frames in
// order via Observe and drives session lifecycle out to a Sink and a Store.
//
// It is NOT safe for concurrent use by itself: one Detector serves one ordered stream (the gRPC
// ingest handler calls Observe inline, per D-B3). Concurrency safety lives in the shared Store.
type Detector struct {
	sink  Sink
	store Store
	cfg   Config

	active  bool    // true while a session is open
	cur     *Record // the open session (nil when idle)
	started bool    // OnSessionStart already emitted (session confirmed past MinSession)

	lastInRaceTs time.Time // recv_timestamp of the most recent in-race frame (becomes EndTime)
	closePending bool      // an in_race=false run is being debounced toward a close
	closeStart   time.Time // recv_timestamp of the first false frame in that run
}

// NewDetector returns a Detector wired to sink and store. Zero-value cfg fields are filled with the
// empirical defaults; NewID defaults to a monotonic "sess-N" counter (unique per Detector instance).
func NewDetector(sink Sink, store Store, cfg Config) *Detector {
	if cfg.Debounce <= 0 {
		cfg.Debounce = defaultDebounce
	}
	if cfg.MinSession <= 0 {
		cfg.MinSession = defaultMinSession
	}
	if cfg.PackageIDResetThreshold == 0 {
		cfg.PackageIDResetThreshold = defaultResetThreshold
	}
	if cfg.NewID == nil {
		n := 0
		cfg.NewID = func() string {
			n++
			return "sess-" + strconv.Itoa(n)
		}
	}
	return &Detector{sink: sink, store: store, cfg: cfg}
}

// Observe advances the state machine by one frame. Frames must arrive in stream order.
func (d *Detector) Observe(f *telemetryv1.TelemetryFrame) {
	ts := f.GetRecvTimestamp().AsTime()
	inRace := f.GetInRace()

	if !d.active {
		// Idle: a session opens on the first in-race frame; menu frames are ignored.
		if inRace {
			d.open(f, ts)
		}
		return
	}

	// Active. Defensive boundaries apply only to IN-RACE frames: a real car swap or package_id reset
	// happens while racing, so a fresh session opens immediately. in_race=false frames must never
	// force a boundary here — menu/results frames (which can report car_id=0) have to take the normal
	// debounce path, or they would bypass the debounce that bridges the finish-line results flap.
	// Neither fired in the capture, but they guard against replay/reconnect and mid-stream car changes.
	if inRace && (f.GetCarId() != d.cur.CarID || d.isReset(f.GetPackageId())) {
		d.finalize()
		d.open(f, ts)
		return
	}

	if inRace {
		d.closePending = false // any in-race frame cancels a pending close (bridges brief dropouts)
		d.attribute(f, ts)
		d.maybeConfirm(ts)
		if d.started {
			d.sink.OnFrame(d.cur.ID, f)
		}
		return
	}

	// in_race=false while active: debounce toward a close.
	if !d.closePending {
		d.closePending = true
		d.closeStart = ts
	}
	if ts.Sub(d.closeStart) >= d.cfg.Debounce {
		d.finalize()
	}
}

// Close finalizes any open session. The gRPC ingest server calls it when the stream ends (EOF or
// error) so an in-progress session is persisted rather than lost.
func (d *Detector) Close() {
	if d.active {
		d.finalize()
	}
}

// open starts a new pending session from an in-race frame and attributes that frame to it. Nothing
// is emitted or stored until the session confirms (see maybeConfirm).
func (d *Detector) open(f *telemetryv1.TelemetryFrame, ts time.Time) {
	d.cur = &Record{
		ID:             d.cfg.NewID(),
		StartTime:      ts,
		CarID:          f.GetCarId(),
		CarName:        f.GetCarName(),
		Source:         f.GetSource(),
		FirstPackageID: f.GetPackageId(),
		Status:         StatusActive,
	}
	d.active = true
	d.started = false
	d.closePending = false
	d.attribute(f, ts)
}

// attribute folds one in-race frame into the current record: counts, gap tally, package id, and the
// lap snapshot (kept current so that at close it reflects the LAST in-race frame — see Record).
func (d *Detector) attribute(f *telemetryv1.TelemetryFrame, ts time.Time) {
	d.cur.FrameCount++
	if f.GetGap() {
		d.cur.GapCount++
	}
	d.lastInRaceTs = ts
	d.cur.LastPackageID = f.GetPackageId()
	d.cur.CurrentLap = f.GetCurrentLap()
	d.cur.TotalLaps = f.GetTotalLaps()
	d.cur.BestLap = f.GetBestLap().AsDuration() // nil-safe: yields 0 when unset
	d.cur.LastLap = f.GetLastLap().AsDuration()
}

// maybeConfirm emits OnSessionStart (once) and persists the active record the moment the session has
// lasted at least MinSession. Sessions that close before this never confirm and are dropped silently
// — this is what discards the finish-line results flap.
func (d *Detector) maybeConfirm(ts time.Time) {
	if d.started {
		return
	}
	if ts.Sub(d.cur.StartTime) >= d.cfg.MinSession {
		if err := d.store.Save(d.cur); err != nil {
			// Do not confirm: emitting OnSessionStart/OnFrame for a session that was never persisted
			// would cascade into FK-violating lap inserts downstream. Surface and skip.
			d.onError(fmt.Errorf("save active session %s: %w", d.cur.ID, err))
			return
		}
		d.started = true
		d.sink.OnSessionStart(d.cur)
	}
}

// onError surfaces a persistence failure via the configured hook (no-op if unset).
func (d *Detector) onError(err error) {
	if d.cfg.OnError != nil {
		d.cfg.OnError(err)
	}
}

// finalize closes the current session and resets to idle. A confirmed session gets its EndTime (the
// last in-race frame), its outcome tag, a final Save, and an OnSessionEnd. An unconfirmed session is
// dropped without any emission.
func (d *Detector) finalize() {
	if d.cur != nil && d.started {
		d.cur.EndTime = d.lastInRaceTs
		d.cur.Status = classify(d.cur)
		if err := d.store.Save(d.cur); err != nil {
			// The session row already exists (saved active at confirm); only the final update failed.
			d.onError(fmt.Errorf("save final session %s: %w", d.cur.ID, err))
		}
		d.sink.OnSessionEnd(d.cur)
	}
	d.cur = nil
	d.active = false
	d.started = false
	d.closePending = false
}

// isReset reports a defensive package_id reset: a backward jump larger than the threshold. package_id
// is normally monotonic (D-B2), so this only fires on replay/reconnect counter resets.
func (d *Detector) isReset(pkg uint32) bool {
	return pkg < d.cur.LastPackageID && d.cur.LastPackageID-pkg > d.cfg.PackageIDResetThreshold
}

// classify sets the outcome tag (D-B7): abandoned when the event's lap target was not reached
// (current_lap < total_laps), otherwise finished. Reading lap counters here is lifecycle bookkeeping,
// not race analysis (the SM scope boundary, D-B7).
func classify(r *Record) Status {
	if r.TotalLaps > 0 && r.CurrentLap < r.TotalLaps {
		return StatusAbandoned
	}
	return StatusFinished
}
