package processing

import (
	"fmt"
	"math"
	"sync"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// defaultTraceBatch bounds how many samples buffer before a mid-lap flush, so memory stays bounded on
// very long laps. Otherwise a lap's samples flush at its boundary. ~1200 ≈ 20s at 60Hz.
const defaultTraceBatch = 1200

// TraceRecorder captures each lap's canonical-frame trace. It implements session.Sink: it buffers
// samples per session, computes cumulative in-lap distance from position deltas at ingest, and
// bulk-flushes to a TraceStore on the lap boundary / buffer cap / session end — never per frame, so
// the gRPC hot path is not blocked by a DB write on every frame.
type TraceRecorder struct {
	store TraceStore
	batch int

	mu    sync.Mutex
	state map[string]*traceState

	// OnError, if set, surfaces a batch-persist failure (the Sink API is error-less). nil = ignore.
	OnError func(error)

	// DeltaFn, if set, supplies the live delta-to-best (ns, ok) captured for the current frame; wired
	// to live.DeltaTracker.Delta by the app. nil = no per-sample delta.
	DeltaFn func() (int64, bool)
}

type traceState struct {
	started   bool
	lapNumber int32
	distance  float64 // cumulative metres within the current lap (reset at each lap boundary)
	havePrev  bool
	prevX     float32
	prevZ     float32
	buf       []Sample
}

// fuelPct returns current fuel as a percentage of tank capacity, or 0 when capacity is unknown/zero
// (some cars report no fuel data) to avoid a division by zero. Reimplemented here rather than imported
// from rtps/live so processing carries no dependency on the live package (mirrors live.fuelPct).
func fuelPct(f *telemetryv1.TelemetryFrame) float32 {
	capacity := f.GetFuelCapacity()
	if capacity <= 0 {
		return 0
	}
	return f.GetCurrentFuel() / capacity * 100
}

// NewTraceRecorder returns a recorder writing to store.
func NewTraceRecorder(store TraceStore) *TraceRecorder {
	return &TraceRecorder{store: store, batch: defaultTraceBatch, state: make(map[string]*traceState)}
}

// OnSessionStart begins fresh trace state for the session.
func (r *TraceRecorder) OnSessionStart(rec *session.Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state[rec.ID] = &traceState{}
}

// OnFrame appends one sample, advancing cumulative in-lap distance. On a lap increment it detaches the
// completed lap's remaining samples for flushing and resets distance; a buffer-cap detach keeps memory
// bounded without resetting distance (mid-lap). The actual DB write happens OUTSIDE the mutex (see
// save) so a slow batch never blocks the ingest hot path / other sessions.
func (r *TraceRecorder) OnFrame(sessionID string, f *telemetryv1.TelemetryFrame) {
	r.mu.Lock()
	batch := r.appendLocked(sessionID, f)
	r.mu.Unlock()
	r.save(batch)
}

// appendLocked does the state mutation under the caller's lock and returns any batch that should be
// flushed (nil if none). It never touches the store.
func (r *TraceRecorder) appendLocked(sessionID string, f *telemetryv1.TelemetryFrame) []Sample {
	st := r.state[sessionID]
	if st == nil {
		return nil // no active session (frame before start / after end) — drop, mirroring the segmenter
	}

	lap := f.GetCurrentLap()
	var batch []Sample
	if !st.started {
		st.started = true
		st.lapNumber = lap
	} else if lap > st.lapNumber { // lap boundary (monotonic; a decrease is ignored)
		batch = st.detach()
		st.lapNumber = lap
		st.distance = 0
		st.havePrev = false
	}

	x, z := f.GetPositionX(), f.GetPositionZ()
	if st.havePrev {
		dx, dz := float64(x-st.prevX), float64(z-st.prevZ)
		st.distance += math.Hypot(dx, dz)
	}
	st.prevX, st.prevZ, st.havePrev = x, z, true

	s := Sample{
		RecvTime:    f.GetRecvTimestamp().AsTime(),
		SessionID:   sessionID,
		LapNumber:   lap,
		Seq:         f.GetSequence(),
		Distance:    st.distance,
		PosX:        x,
		PosY:        f.GetPositionY(),
		PosZ:        z,
		Speed:       f.GetCarSpeed(),
		Throttle:    f.GetThrottle(),
		Brake:       f.GetBrake(),
		RPM:         f.GetRpm(),
		Gear:        int32(f.GetCurrentGear()),
		Yaw:         f.GetRotationYaw(),
		AngVelY:     f.GetAngularVelocityY(),
		Fuel:        fuelPct(f),
		TyreTempFL:  f.GetTyreTempFl(),
		TyreTempFR:  f.GetTyreTempFr(),
		TyreTempRL:  f.GetTyreTempRl(),
		TyreTempRR:  f.GetTyreTempRr(),
		Clutch:      f.GetClutch(),
		Boost:       f.GetBoost(),
		SurfaceType: f.GetSurfaceType(),                                                             // per-wheel on/off-track material code; drives sector invalidation
		Steering:    (f.GetWheelSteeringAngleFrontLeft() + f.GetWheelSteeringAngleFrontRight()) / 2, // radians; 0 on format-A
	}
	// Ingest publishes to live BEFORE it Observe()s the segmenter (ingest.go: Publish then Observe), so
	// DeltaFn (= live.DeltaTracker.Delta) already reflects THIS frame when OnFrame runs.
	if r.DeltaFn != nil {
		if ns, ok := r.DeltaFn(); ok {
			v := ns
			s.DeltaToBestNs = &v
		}
	}
	st.buf = append(st.buf, s)
	// A boundary detach empties the buffer, so the cap can't also trip in the same call — at most one
	// batch is returned per frame.
	if len(st.buf) >= r.batch {
		batch = st.detach()
	}
	return batch
}

// OnSessionEnd detaches and flushes the final buffered samples.
func (r *TraceRecorder) OnSessionEnd(rec *session.Record) {
	r.mu.Lock()
	var batch []Sample
	if st := r.state[rec.ID]; st != nil {
		batch = st.detach()
		delete(r.state, rec.ID)
	}
	r.mu.Unlock()
	r.save(batch)
}

// detach hands off the buffered samples and clears the buffer (distance/lap state untouched, so a
// mid-lap detach is transparent). Caller must hold r.mu.
func (st *traceState) detach() []Sample {
	b := st.buf
	st.buf = nil
	return b
}

// save bulk-writes a batch to the store, OUTSIDE the mutex. Best-effort: on failure the batch is
// dropped and surfaced via OnError (bounded retry/backpressure is a future concern).
func (r *TraceRecorder) save(batch []Sample) {
	if len(batch) == 0 {
		return
	}
	if err := r.store.SaveBatch(batch); err != nil && r.OnError != nil {
		r.OnError(fmt.Errorf("save trace batch: %w", err))
	}
}
