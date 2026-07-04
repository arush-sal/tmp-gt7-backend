package processing

import (
	"sync"
	"time"
)

// Sample is one canonical frame captured into a lap's telemetry trace. Distance is the cumulative
// in-lap distance (metres) computed at ingest; normalize 0..1 on read against the lap's max distance.
// The backend reasons only about canonical frames, never wire packets — Sample mirrors the frame
// fields the trace needs (position for distance/track, speed/pedals for coaching, yaw/ang-vel for
// corner detection).
type Sample struct {
	RecvTime  time.Time
	SessionID string
	LapNumber int32
	Seq       uint64
	Distance  float64
	PosX      float32
	PosY      float32
	PosZ      float32
	Speed     float32
	Throttle  float32
	Brake     float32
	RPM       float32
	Gear      int32
	Yaw       float32
	AngVelY   float32
}

// TraceStore persists per-frame trace samples in batches (the recorder buffers per lap and bulk-loads,
// never per-frame — inline per-frame DB writes would block the gRPC hot path). Slice ships an
// in-memory store; the Postgres impl targets the lap_trace hypertable.
type TraceStore interface {
	// SaveBatch bulk-inserts a batch of samples.
	SaveBatch(samples []Sample) error
	// ByLap returns a lap's samples ordered by seq (for Analysis/dashboard reads). It returns an error
	// so DB read failures surface rather than looking like "no data".
	ByLap(sessionID string, lapNumber int32) ([]Sample, error)
}

// MemTraceStore is a goroutine-safe in-memory TraceStore.
type MemTraceStore struct {
	mu      sync.RWMutex
	samples []Sample
}

// NewMemTraceStore returns an empty in-memory trace store.
func NewMemTraceStore() *MemTraceStore { return &MemTraceStore{} }

// SaveBatch appends copies of the samples (the recorder reuses its buffer, so we must copy).
func (m *MemTraceStore) SaveBatch(samples []Sample) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.samples = append(m.samples, samples...)
	return nil
}

// ByLap returns the samples for one lap in insertion (seq) order. The error is always nil (in-memory);
// it exists to satisfy the TraceStore contract.
func (m *MemTraceStore) ByLap(sessionID string, lapNumber int32) ([]Sample, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Sample
	for _, s := range m.samples {
		if s.SessionID == sessionID && s.LapNumber == lapNumber {
			out = append(out, s)
		}
	}
	return out, nil
}
