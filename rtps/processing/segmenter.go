package processing

import (
	"errors"
	"fmt"
	"sync"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// LapSegmenter turns a session's frame stream into Laps. It implements session.Sink, so Session
// Management drives it: OnSessionStart resets per-session state, OnFrame advances the current lap and
// emits a lap on each lap-counter increment, OnSessionEnd flushes the final (partial) lap. A first lap
// the session joined mid-way is never stored (see lapState.partial).
//
// It keeps state per session id (a shared segmenter may serve concurrent streams), guarded by a mutex
// since Sink methods are called on each stream's goroutine.
type LapSegmenter struct {
	store LapStore
	mu    sync.Mutex
	state map[string]*lapState

	// OnError, if set, is called when persisting a lap fails (the Sink API is error-less, so this
	// surfaces store failures that would otherwise be silent). nil = ignore.
	OnError func(error)

	// TrackFn, if set, reports the live detector's current track for a session (slug, confidence; ""
	// when none) and is read as each lap is stored, so every lap carries the track it was driven on
	// (TrackDetector.Track). nil = laps are stored unlabelled.
	TrackFn func(sessionID string) (slug, confidence string)

	// OnSaved, if set, is called with each lap after it is stored (once per lap, on the stream
	// goroutine), so the live stored-reference loader can pick up a lap that may be the track's new
	// reference (live.RefLoader.LapStored). nil = no notification.
	OnSaved func(lap Lap)
}

// lapState tracks the lap currently being accumulated for one session.
type lapState struct {
	open       bool
	lapNumber  int32
	startTime  time.Time
	lastTime   time.Time // recv_timestamp of the most recent frame (the lap's running end)
	frameCount uint64
	gapCount   uint64
	// partial: the session's first frame was already on a timed lap (lap >= 1), i.e. it joined that lap
	// mid-way (Record back on, a replay loop start, a reconnect). That lap's trace is partial even though
	// GT7's last_lap at its rollover is a full lap time, so it is discarded: no lap is emitted for it,
	// neither at its rollover nor at session end (TraceRecorder likewise stores none of its samples).
	// Mirrors the Detector's partialLap, which likewise does not count it for rule B, so a session that
	// ends on it is dropped anyway.
	partial bool
	// completed: a completed timed lap was already emitted. The first one is the lap the Detector's
	// dedup decides on, so a taken fingerprint there means a missed duplicate (see emit).
	completed bool
}

// NewLapSegmenter returns a segmenter that writes laps to store.
func NewLapSegmenter(store LapStore) *LapSegmenter {
	return &LapSegmenter{store: store, state: make(map[string]*lapState)}
}

// OnSessionStart begins fresh lap tracking for the session.
func (s *LapSegmenter) OnSessionStart(rec *session.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[rec.ID] = &lapState{}
}

// OnFrame folds one in-race frame into the current lap. GT7's current_lap increments at each lap
// line; on a change we close the accumulated lap (its time is GT7's last_lap — the just-completed
// lap) and open the next. Only in-race frames of confirmed sessions reach here (per the Detector).
func (s *LapSegmenter) OnFrame(sessionID string, f *telemetryv1.TelemetryFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.state[sessionID]
	if st == nil {
		// No active session for this id: a frame before OnSessionStart or after OnSessionEnd (both
		// contract violations the Detector never commits). Drop it rather than resurrect state.
		return
	}

	lap := f.GetCurrentLap()
	ts := f.GetRecvTimestamp().AsTime()

	switch {
	case !st.open:
		st.open = true
		st.lapNumber = lap
		st.startTime = ts
		st.partial = lap >= 1
	case lap > st.lapNumber:
		// Lap boundary: current_lap advanced. Close the accumulated lap — its time is the
		// just-completed lap's last_lap. current_lap is monotonic within a session; a decrease is
		// anomalous and ignored (the frame is attributed to the current lap, no split).
		if st.partial {
			st.partial = false // joined mid-lap: discard the lap, nothing is stored for it
		} else {
			var fp string
			if st.lapNumber >= 1 { // a completed timed lap (not the formation lap): fingerprint it
				fp = session.LapFingerprint(f, st.lapNumber)
			}
			s.emit(sessionID, st, true, f.GetLastLap().AsDuration(), fp)
		}
		st.lapNumber = lap
		st.startTime = ts
		st.frameCount = 0
		st.gapCount = 0
	}

	st.frameCount++
	if f.GetGap() {
		st.gapCount++
	}
	st.lastTime = ts
}

// OnSessionEnd flushes the final, still-open lap as incomplete (the session ended mid-lap), unless it
// is the joined-mid-lap first lap (see lapState.partial), which is never stored.
func (s *LapSegmenter) OnSessionEnd(rec *session.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.state[rec.ID]; st != nil && st.open && !st.partial {
		s.emit(rec.ID, st, false, 0, "")
	}
	delete(s.state, rec.ID)
}

// emit builds and stores one Lap from the accumulated state. complete=true means it was closed by a
// lap-counter increment (use the supplied last_lap as its duration); otherwise the duration falls
// back to the wall-clock span. fp is the lap's fingerprint ("" when it has none). A lap is Valid for Personal-Best only if it is a timed, complete,
// gap-free lap.
func (s *LapSegmenter) emit(sessionID string, st *lapState, complete bool, lastLap time.Duration, fp string) {
	dur := lastLap
	fallback := false
	if dur <= 0 { // formation lap, or the partial final lap: no authoritative last_lap
		dur = st.lastTime.Sub(st.startTime)
		fallback = true
	}
	timed := st.lapNumber > 0
	lap := &Lap{
		SessionID:   sessionID,
		LapNumber:   st.lapNumber,
		StartTime:   st.startTime,
		EndTime:     st.lastTime,
		Duration:    dur,
		FrameCount:  st.frameCount,
		GapCount:    st.gapCount,
		Complete:    complete,
		Timed:       timed,
		Valid:       timed && complete && st.gapCount == 0,
		Fallback:    fallback,
		Fingerprint: fp,
	}
	if s.TrackFn != nil {
		lap.TrackSlug, lap.TrackConfidence = s.TrackFn(sessionID)
	}
	err := s.store.Save(lap)
	first := fp != "" && !st.completed
	if fp != "" {
		st.completed = true
	}
	if errors.Is(err, ErrDuplicateFingerprint) {
		// Another session already owns this lap's fingerprint. Keep the lap, drop its fingerprint (the
		// lap data matters; the fingerprint is only a dedup key). Only a session's first completed lap
		// decides duplicates (the Detector), so on a later lap this is the expected late duplicate; on
		// the first it means the Detector missed one (e.g. its lookup failed): surface that.
		lap.Fingerprint = ""
		if err = s.store.Save(lap); err == nil && first && s.OnError != nil {
			s.OnError(fmt.Errorf("save lap %s#%d: first completed lap already stored under another session (duplicate session kept): %w",
				sessionID, st.lapNumber, ErrDuplicateFingerprint))
		}
	}
	if err != nil {
		if s.OnError != nil {
			s.OnError(fmt.Errorf("save lap %s#%d: %w", sessionID, st.lapNumber, err))
		}
		return
	}
	if s.OnSaved != nil {
		s.OnSaved(*lap)
	}
}
