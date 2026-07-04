package processing

import (
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mkTraceFrame builds a frame with the position (x,z) + lap + seq the recorder needs.
func mkTraceFrame(ms int, lap int32, seq uint64, x, z float32) *telemetryv1.TelemetryFrame {
	return &telemetryv1.TelemetryFrame{
		RecvTimestamp: timestamppb.New(segBase.Add(time.Duration(ms) * time.Millisecond)),
		CurrentLap:    lap,
		Sequence:      seq,
		PositionX:     x,
		PositionZ:     z,
	}
}

func record(store TraceStore, frames ...*telemetryv1.TelemetryFrame) *TraceRecorder {
	r := NewTraceRecorder(store)
	rec := &session.Record{ID: "s1"}
	r.OnSessionStart(rec)
	for _, f := range frames {
		r.OnFrame("s1", f)
	}
	r.OnSessionEnd(rec)
	return r
}

// TestTraceRecorder_DistancePerLap: cumulative in-lap distance is the running sum of position deltas
// and resets to 0 at each lap boundary.
func TestTraceRecorder_DistancePerLap(t *testing.T) {
	store := NewMemTraceStore()
	record(store,
		mkTraceFrame(0, 1, 1, 0, 0),     // first frame: distance 0
		mkTraceFrame(100, 1, 2, 3, 0),   // +3
		mkTraceFrame(200, 1, 3, 3, 4),   // +4 -> 7
		mkTraceFrame(300, 2, 4, 10, 10), // new lap: distance resets to 0
		mkTraceFrame(400, 2, 5, 13, 10), // +3
	)

	lap1, _ := store.ByLap("s1", 1)
	got := []float64{}
	for _, s := range lap1 {
		got = append(got, s.Distance)
	}
	want := []float64{0, 3, 7}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("lap1 distances = %v, want %v", got, want)
	}

	lap2, _ := store.ByLap("s1", 2)
	if len(lap2) != 2 || lap2[0].Distance != 0 || lap2[1].Distance != 3 {
		t.Errorf("lap2 distances = %v, want [0 3] (reset at boundary)", lap2)
	}
}

// batchSpy records every SaveBatch call so tests can assert flush behavior.
type batchSpy struct {
	batches int
	all     []Sample
}

func (b *batchSpy) SaveBatch(s []Sample) error            { b.batches++; b.all = append(b.all, s...); return nil }
func (b *batchSpy) ByLap(string, int32) ([]Sample, error) { return nil, nil }

// TestTraceRecorder_BatchFlush: samples flush in batches (buffer cap) and nothing is lost across the
// cap flushes + the final session-end flush.
func TestTraceRecorder_BatchFlush(t *testing.T) {
	spy := &batchSpy{}
	r := NewTraceRecorder(spy)
	r.batch = 2 // small cap to force mid-lap flushes
	rec := &session.Record{ID: "s1"}
	r.OnSessionStart(rec)
	for i := 0; i < 5; i++ {
		r.OnFrame("s1", mkTraceFrame(i*100, 1, uint64(i+1), float32(i), 0))
	}
	r.OnSessionEnd(rec)

	if len(spy.all) != 5 {
		t.Errorf("persisted %d samples, want 5", len(spy.all))
	}
	if spy.batches < 2 {
		t.Errorf("batches = %d, want >= 2 (cap forced multiple flushes)", spy.batches)
	}
}

// TestTraceRecorder_DropsStrayFrames: frames before OnSessionStart or after OnSessionEnd are dropped
// (no state resurrection), mirroring the segmenter.
func TestTraceRecorder_DropsStrayFrames(t *testing.T) {
	store := NewMemTraceStore()
	r := NewTraceRecorder(store)
	r.OnFrame("ghost", mkTraceFrame(0, 1, 1, 0, 0)) // before any start
	rec := &session.Record{ID: "s1"}
	r.OnSessionStart(rec)
	r.OnFrame("s1", mkTraceFrame(0, 1, 1, 0, 0))
	r.OnSessionEnd(rec)
	r.OnFrame("s1", mkTraceFrame(100, 1, 2, 1, 0)) // after end

	s1, _ := store.ByLap("s1", 1)
	if len(s1) != 1 {
		t.Errorf("s1 lap1 samples = %d, want 1 (stray frames dropped)", len(s1))
	}
	ghost, _ := store.ByLap("ghost", 1)
	if len(ghost) != 0 {
		t.Errorf("ghost samples = %d, want 0", len(ghost))
	}
}
