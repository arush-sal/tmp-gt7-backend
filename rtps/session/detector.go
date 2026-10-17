package session

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/google/uuid"
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

	// minMovingSpeedKPH is the car_speed (km/h) a pending session must exceed at least once before it
	// confirms. Stationary on-track idle (sitting on the grid or in the pit box with lap >= 0) reports
	// exactly 0 in the captures, so any small positive threshold separates it; 5 km/h (walking pace)
	// leaves headroom for physics jitter while being far below any real driving speed.
	minMovingSpeedKPH = 5

	// clockRewindThreshold is how far GT7's in-game clock (canonical time_on_track, which is the
	// time-of-day, not a lap/session timer) must jump BACKWARDS between on-track frames, both on timed
	// laps (>= 1), to count as a replay rewind/loop and split the session. Live driving on timed laps
	// advances it monotonically. The legitimate backward jumps seen in captures all involve lap 0 and
	// are excluded by the lap >= 1 condition: ~99.8s DURING the WTC rolling-start lap 0, ~19s at the
	// lap 0 -> 1 rolling start, and the post-race results screen replaying the race start at lap 0.
	// 30s still leaves margin over the 19s case; replay rewinds jump by whole laps (> 60s).
	clockRewindThreshold = 30 * time.Second

	// clockWrapThreshold: time_on_track is GT7's time of day, which presumably wraps at midnight
	// (86,400,000 ms). A backward jump larger than this is read as the clock passing midnight, not a
	// rewind. UNVERIFIED: none of the 16 captures reaches midnight (max seen 72,060,000 ms = 20:01), so
	// the wrap itself has never been observed; this guard only prevents a false split if it happens.
	clockWrapThreshold = 23 * time.Hour
)

// DiscardReason says why a confirmed session was discarded (rolled back) instead of stored.
type DiscardReason string

const (
	// DiscardDuplicate means a completed lap's fingerprint is already stored under another session (an
	// in-game replay of stored laps, or the same capture ingested twice).
	DiscardDuplicate DiscardReason = "duplicate"
	// DiscardNoCompletedLap means the session ended without a single completed timed lap (no lap >= 1
	// rollover), e.g. a replay fragment, a results screen, or a quit during lap 1.
	DiscardNoCompletedLap DiscardReason = "no completed lap"
	// DiscardRecordingOff means the user switched recording off (RecordingControl) during the session.
	DiscardRecordingOff DiscardReason = "recording off"
)

// OnTrack reports whether f is a driving frame. GT7's in_race flag (flags bit 0) used to mark this,
// but newer game builds leave it clear even while racing, so a lap counter >= 0 also counts: menus
// report current_lap = -1. Shared by every consumer that gates on "the car is on track".
func OnTrack(f *telemetryv1.TelemetryFrame) bool {
	return f.GetInRace() || f.GetCurrentLap() >= 0
}

// LapFingerprint identifies a completed timed lap across sessions: GT7 replays are content-identical
// copies of earlier frames (only package_id differs), so the same lap replayed hashes the same. f is the
// first frame of lap completedLap+1 (the rollover frame, which carries the completed lap's last_lap).
// Inputs: car, lap target, completed lap number, its last_lap (ms), the in-game clock (ms) and the exact
// float32 position bits at that frame. Returns the hex of the first 16 bytes of a SHA-256.
func LapFingerprint(f *telemetryv1.TelemetryFrame, completedLap int32) string {
	in := struct {
		Car, Total, Lap int32
		LastMs, ClockMs int64
		X, Y, Z         uint32
	}{
		f.GetCarId(), f.GetTotalLaps(), completedLap,
		f.GetLastLap().AsDuration().Milliseconds(), f.GetTimeOnTrack().AsDuration().Milliseconds(),
		math.Float32bits(f.GetPositionX()), math.Float32bits(f.GetPositionY()), math.Float32bits(f.GetPositionZ()),
	}
	h := sha256.New()
	_ = binary.Write(h, binary.LittleEndian, &in) // fixed-size struct into a hash: cannot fail
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// isRollover reports a completed timed lap: the lap counter advancing from a lap >= 1 (lap 0 is the
// formation/out lap, never fingerprinted). Mirrors the lap segmenter's boundary rule.
func isRollover(prevLap, lap int32) bool { return prevLap >= 1 && lap > prevLap }

// Config tunes the Detector. Zero-value fields take the defaults above, so Config{} is valid.
type Config struct {
	Debounce                time.Duration // how long in_race must hold an edge before it takes effect
	MinSession              time.Duration // a session shorter than this at close is dropped as noise
	PackageIDResetThreshold uint32        // backward package_id jump beyond this = defensive reset
	NewID                   func() string // session id generator; injected for deterministic tests (default: uuid.NewString)
	// OnError, if set, is called when persisting a session fails. The Sink/Observe API is error-less,
	// so this surfaces store failures (e.g. DB down) that would otherwise be silent. nil = ignore
	// (fine for MemStore, which never errors).
	OnError func(error)

	// FingerprintOwner, if set, enables duplicate-session dropping: it returns the stored session that
	// already owns a lap fingerprint (see LapFingerprint). nil disables dedup.
	FingerprintOwner func(fp string) (sessionID string, found bool)
	// OnTotalLaps is called after a confirmed session's changed positive total_laps was persisted (see
	// Detector.attribute), once per persisted change, on the stream goroutine. The app uses it to
	// re-resolve a live reference that was loaded while the total was unknown. nil = ignore.
	OnTotalLaps func(rec *Record)
	// OnDiscard is called once when a confirmed session is discarded, after its session row was
	// deleted and the sinks were told it ended (see Detector.discard). The app purges the session's laps
	// and traces here. duplicateOf is the original session for DiscardDuplicate, else "".
	OnDiscard func(rec *Record, reason DiscardReason, duplicateOf string)
	// OnDuplicateDiverged is called at most once per dropped session when a later completed lap's
	// fingerprint is not owned by originalID (the replay extends beyond the stored data). The session
	// stays dropped.
	OnDuplicateDiverged func(rec *Record, originalID string)
	// OnLateDuplicate is called at most once per kept session when a completed lap AFTER its first one
	// matches another stored session. Only the first completed lap decides duplicates, so the session is
	// kept; the lap segmenter stores that lap without its (already taken) fingerprint.
	OnLateDuplicate func(rec *Record, otherID string)

	// Recording, if set, is the user's Record switch for this stream (see RecordingControl). nil means
	// always recording. Only the in-process source's Detector gets one.
	Recording *RecordingControl
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

	moved     bool          // the open session has exceeded minMovingSpeedKPH at least once
	clock     time.Duration // in-game clock (time_on_track) of the last on-track frame
	dupOf     string        // non-empty once the open session is a dropped duplicate of this session
	diverged  bool          // OnDuplicateDiverged already reported for this dropped session
	completed bool          // a completed timed lap (lap >= 1 rollover) was seen after confirmation
	discarded bool          // the open session was rolled back; its remaining frames are not persisted
	notRec    bool          // the user switched recording off for the current stint (resets at its end)
	// partialLap is the lap the session was confirmed on when that lap is >= 1: the session joined it
	// mid-lap (Record back on, a replay loop start, a reconnect), so its rollover is NOT a completed
	// timed lap: no fingerprint, no dedup decision, does not satisfy rule B. 0 when confirmed on lap 0.
	partialLap   int32
	dedupDecided bool // the first completed lap's dedup decision was made (later laps never discard)
	lateWarned   bool // OnLateDuplicate already reported

	// savedTotal is the total_laps last SUCCESSFULLY persisted for the open session (d.cur.TotalLaps is
	// the latest seen); nextTotalSave throttles retries after a failed Save, by frame timestamp.
	savedTotal    int32
	nextTotalSave time.Time

	lastInRaceTs time.Time // recv_timestamp of the most recent in-race frame (becomes EndTime)
	closePending bool      // an in_race=false run is being debounced toward a close
	closeStart   time.Time // recv_timestamp of the first false frame in that run
}

// NewDetector returns a Detector wired to sink and store. Zero-value cfg fields are filled with the
// empirical defaults; NewID defaults to uuid.NewString, giving globally-unique session ids.
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
		// uuid.NewString gives collision-free ids across process restarts. The old monotonic "sess-N"
		// counter reset to sess-1 every restart, so the same id mapped to different races across runs.
		cfg.NewID = uuid.NewString
	}
	return &Detector{sink: sink, store: store, cfg: cfg}
}

// Observe advances the state machine by one frame. Frames must arrive in stream order.
func (d *Detector) Observe(f *telemetryv1.TelemetryFrame) {
	ts := f.GetRecvTimestamp().AsTime()
	inRace := OnTrack(f) // "in race" below means on track: see OnTrack
	if d.cfg.Recording != nil {
		d.applyRecording() // lock-free unless a request is queued
	}

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
	//
	// A replay loop or rewind is also a boundary (see isRewind). The 1-2 lap=-1 frames GT7 shows
	// between replay loops are shorter than the close debounce, so without it the loops would merge.
	if inRace && (f.GetCarId() != d.cur.CarID || d.isReset(f.GetPackageId()) || d.isRewind(f)) {
		d.finalize()
		d.open(f, ts)
		return
	}

	if inRace {
		d.closePending = false // any in-race frame cancels a pending close (bridges brief dropouts)
		prevLap := d.cur.CurrentLap
		d.attribute(f, ts)
		d.maybeConfirm(f, ts)
		if d.started && isRollover(prevLap, f.GetCurrentLap()) {
			if prevLap == d.partialLap {
				d.partialLap = 0 // the joined-mid-lap first lap: not a completed timed lap
			} else {
				d.completed = true
				d.checkDuplicate(f, prevLap) // before forwarding: a duplicate's rollover frame is not persisted
			}
		}
		if d.started && !d.discarded {
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

// applyRecording applies a queued RecordingControl request before the current frame is processed.
// Off: the open session is discarded (or, if not yet confirmed, marked so it never confirms) and the
// rest of the stint is swallowed. On: the swallowed stint is closed without emission, so this frame
// (if on track) opens a fresh session with a new id under the normal confirm rules. An off request
// targeting a session that is no longer the open one (it ended and another opened after the caller's
// check) is answered ErrSessionChanged and changes nothing.
func (d *Detector) applyRecording() {
	r := d.cfg.Recording.take()
	if r == nil {
		return
	}
	defer close(r.done)
	if !d.active {
		if !r.on {
			r.err = ErrNoSession
		}
		return
	}
	if !r.on && r.expect != "" && r.expect != d.cur.ID && (d.dupOf == "" || r.expect != d.dupOf) {
		r.err = ErrSessionChanged
		return
	}
	on := r.on
	switch {
	case !on && !d.notRec:
		d.notRec = true
		if d.active && !d.discarded {
			if d.started {
				d.discard(DiscardRecordingOff, "")
			} else {
				d.discarded = true
			}
		}
	case on && d.notRec:
		d.notRec = false
		if d.active {
			d.finalize()
		}
	}
	d.publishRecording()
}

// publishRecording mirrors the stream state into the RecordingControl (per session event, never per
// frame): whether recording, whether a session is open, and the open recorded session's id.
func (d *Detector) publishRecording() {
	c := d.cfg.Recording
	if c == nil {
		return
	}
	id, cur := "", ""
	if d.active {
		cur = d.cur.ID
		if !d.discarded {
			id = cur
		}
	}
	c.publish(!d.notRec, d.active, id, cur, d.dupOf)
}

// Close finalizes any open session. The gRPC ingest server calls it when the stream ends (EOF or
// error) so an in-progress session is persisted rather than lost.
func (d *Detector) Close() {
	if d.active {
		d.finalize()
	}
	if d.cfg.Recording != nil {
		d.cfg.Recording.dropPending() // stream end: a queued request can never apply
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
		CarCategory:    f.GetCarCategory(),
		Source:         f.GetSource(),
		FirstPackageID: f.GetPackageId(),
		Status:         StatusActive,
	}
	d.active = true
	d.started = false
	d.savedTotal, d.nextTotalSave = 0, time.Time{}
	d.closePending = false
	d.moved = false
	d.dupOf = ""
	d.diverged = false
	d.completed = false
	d.partialLap = 0
	d.dedupDecided = false
	d.lateWarned = false
	d.discarded = d.notRec // recording off for this stint: the new session is never persisted
	d.attribute(f, ts)
	d.publishRecording()
}

// attribute folds one on-track frame into the current record: counts, gap tally, package id, and the
// lap snapshot. The snapshot only takes CurrentLap/BestLap/LastLap from timed-lap frames (lap >= 1) and
// TotalLaps when > 0: after a race GT7 keeps reporting on-track frames with lap 0 and/or total_laps 0
// (results screen), which must not overwrite the race's counters (see Record). CurrentLap is therefore
// the LAST ON-TRACK TIMED lap, which the split and rollover rules compare against; menu (lap -1) and
// lap-0 frames in between never reset it, so a replay loop 4 -> -1 -> 2 still compares 2 against 4.
func (d *Detector) attribute(f *telemetryv1.TelemetryFrame, ts time.Time) {
	d.cur.FrameCount++
	if f.GetGap() {
		d.cur.GapCount++
	}
	d.lastInRaceTs = ts
	d.cur.LastPackageID = f.GetPackageId()
	if f.GetCurrentLap() >= 1 {
		d.cur.CurrentLap = f.GetCurrentLap()
		d.cur.BestLap = f.GetBestLap().AsDuration() // nil-safe: yields 0 when unset
		d.cur.LastLap = f.GetLastLap().AsDuration()
	}
	if tl := f.GetTotalLaps(); tl > 0 {
		d.cur.TotalLaps = tl
	}
	d.persistTotalLaps(ts)
	d.clock = f.GetTimeOnTrack().AsDuration()
	if f.GetCarSpeed() > minMovingSpeedKPH {
		d.moved = true
	}
}

// isRewind reports a replay loop/rewind boundary on an on-track frame. Both the previous on-track timed
// lap (d.cur.CurrentLap, see attribute) and this frame's lap must be >= 1; then a lap counter decrease
// (replay loop 4 -> 2, rewind 5 -> 3) or the in-game clock jumping backwards by more than
// clockRewindThreshold is a boundary. Lap 0 never splits: the results screen after a finish (lap N ->
// 0) and rolling-start clock jumps during lap 0 belong to the session they follow or precede.
func (d *Detector) isRewind(f *telemetryv1.TelemetryFrame) bool {
	prev, lap := d.cur.CurrentLap, f.GetCurrentLap()
	if prev < 1 || lap < 1 {
		return false
	}
	back := d.clock - f.GetTimeOnTrack().AsDuration()
	return lap < prev || (back > clockRewindThreshold && back < clockWrapThreshold)
}

// checkDuplicate looks up the fingerprint of the lap just completed (prevLap) at its rollover frame f.
// Only the session's FIRST completed lap decides: if a DIFFERENT stored session owns it, the open
// session is a duplicate of that session and is discarded (see discard); nothing further of it is
// persisted. A later lap owned by another session never discards (it is reported once via
// OnLateDuplicate and the session is kept). Once dropped, a lap not owned by the original is reported
// once via OnDuplicateDiverged; the session is never resurrected.
func (d *Detector) checkDuplicate(f *telemetryv1.TelemetryFrame, prevLap int32) {
	if d.cfg.FingerprintOwner == nil {
		return
	}
	owner, found := d.cfg.FingerprintOwner(LapFingerprint(f, prevLap))
	other := found && owner != d.cur.ID
	switch {
	case d.dupOf != "":
		if (!found || owner != d.dupOf) && !d.diverged {
			d.diverged = true
			if d.cfg.OnDuplicateDiverged != nil {
				d.cfg.OnDuplicateDiverged(d.cur, d.dupOf)
			}
		}
	case !d.dedupDecided:
		d.dedupDecided = true
		if other {
			d.dupOf = owner
			d.discard(DiscardDuplicate, owner)
		}
	case other && !d.lateWarned:
		d.lateWarned = true
		if d.cfg.OnLateDuplicate != nil {
			d.cfg.OnLateDuplicate(d.cur, owner)
		}
	}
}

// discard is the single rollback path for a confirmed session that must leave no persisted trace. The
// sinks get OnSessionEnd so they flush and forget their per-session state, the session row is deleted,
// and OnDiscard purges what the sinks wrote (laps, traces). The session stays open in discarded mode
// until its natural boundary, so its remaining frames are swallowed instead of opening a new session.
func (d *Detector) discard(reason DiscardReason, duplicateOf string) {
	d.discarded = true
	d.cur.EndTime = d.lastInRaceTs
	d.cur.Status = classify(d.cur)
	d.sink.OnSessionEnd(d.cur)
	if err := d.store.Delete(d.cur.ID); err != nil {
		d.onError(fmt.Errorf("delete discarded session %s: %w", d.cur.ID, err))
	}
	if d.cfg.OnDiscard != nil {
		d.cfg.OnDiscard(d.cur, reason, duplicateOf)
	}
	d.publishRecording()
}

// totalSaveRetry is the minimum gap (by frame timestamp) between attempts to persist a total_laps after a
// failed Save, so a failing database is not hit at the frame rate.
const totalSaveRetry = time.Second

// persistTotalLaps writes a confirmed session's row when its latest total_laps differs from the last
// one successfully persisted. A confirmed session's row must not keep a stale total_laps until it
// closes: lap eligibility (processing.RefEligible: a race's lap 1 is never a reference) reads it from
// the stored row while the session is still active. It writes on CHANGE only (once or twice per
// session, never per frame), with the existing full Save: that is rare enough that a narrower
// UpdateTotalLaps store method would add interface surface for no gain. Unconfirmed sessions are saved
// with the total at confirm (maybeConfirm). A failed Save is surfaced and retried by later frames, at
// most once per totalSaveRetry. OnTotalLaps fires after each successful write.
func (d *Detector) persistTotalLaps(ts time.Time) {
	if !d.started || d.discarded || d.cur.TotalLaps <= 0 || d.cur.TotalLaps == d.savedTotal || ts.Before(d.nextTotalSave) {
		return
	}
	if err := d.store.Save(d.cur); err != nil {
		d.nextTotalSave = ts.Add(totalSaveRetry)
		d.onError(fmt.Errorf("save total_laps for session %s: %w", d.cur.ID, err))
		return
	}
	d.savedTotal = d.cur.TotalLaps
	if d.cfg.OnTotalLaps != nil {
		d.cfg.OnTotalLaps(d.cur)
	}
}

// maybeConfirm emits OnSessionStart (once) and persists the active record the moment the session has
// lasted at least MinSession. Sessions that close before this never confirm and are dropped silently
// — this is what discards the finish-line results flap.
func (d *Detector) maybeConfirm(f *telemetryv1.TelemetryFrame, ts time.Time) {
	if d.started || d.discarded {
		return
	}
	// Must-move: stationary on-track idle (lap >= 0 on the grid / pit box) never becomes a session.
	if d.moved && ts.Sub(d.cur.StartTime) >= d.cfg.MinSession {
		if err := d.store.Save(d.cur); err != nil {
			// Do not confirm: emitting OnSessionStart/OnFrame for a session that was never persisted
			// would cascade into FK-violating lap inserts downstream. Surface and skip.
			d.onError(fmt.Errorf("save active session %s: %w", d.cur.ID, err))
			return
		}
		d.started = true
		d.savedTotal = d.cur.TotalLaps // the confirm Save carried it
		if lap := f.GetCurrentLap(); lap >= 1 {
			d.partialLap = lap // confirmed mid-lap: this first lap is partial (see partialLap)
		}
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
// last in-race frame), its outcome tag, a final Save, and an OnSessionEnd. A confirmed session without
// a completed timed lap is discarded instead (DiscardNoCompletedLap). An unconfirmed session, or one
// already discarded, is dropped without any emission. Recording resets to on.
func (d *Detector) finalize() {
	if d.cur != nil && d.started && !d.discarded && !d.completed {
		d.discard(DiscardNoCompletedLap, "")
	}
	if d.cur != nil && d.started && !d.discarded {
		d.cur.EndTime = d.lastInRaceTs
		d.cur.Status = classify(d.cur)
		if err := d.store.Save(d.cur); err != nil {
			// The session row already exists (saved active at confirm); only the final update failed.
			d.onError(fmt.Errorf("save final session %s: %w", d.cur.ID, err))
		} else if d.cur.TotalLaps > 0 && d.cur.TotalLaps != d.savedTotal {
			// The final Save carried a total whose own save failed and was never retried (the session
			// closed inside the retry throttle): it is persisted now, so OnTotalLaps still fires.
			d.savedTotal = d.cur.TotalLaps
			if d.cfg.OnTotalLaps != nil {
				d.cfg.OnTotalLaps(d.cur)
			}
		}
		d.sink.OnSessionEnd(d.cur)
	}
	if d.cfg.Recording != nil {
		d.cfg.Recording.dropPending() // the session a queued request targeted is gone
	}
	d.cur = nil
	d.active = false
	d.started = false
	d.closePending = false
	d.notRec = false // the Record switch is per session: back ON for whatever comes next
	d.publishRecording()
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
