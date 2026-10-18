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
		mkTraceFrame(0, 0, 0, 0, 0),     // formation lap, so lap 1 is recorded (not joined mid-lap)
		mkTraceFrame(0, 1, 1, 0, 0),     // lap 1 first frame: distance 0
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

// TestTraceRecorder_PartialFirstLapDiscarded: a session whose first frame is already on a timed lap
// joined it mid-way; that lap's samples are never stored (the segmenter stores no lap for it either).
// The next lap is recorded from its first frame with distance starting at 0.
func TestTraceRecorder_PartialFirstLapDiscarded(t *testing.T) {
	spy := &batchSpy{}
	record(spy,
		mkTraceFrame(0, 2, 1, 0, 0), // joined mid lap 2
		mkTraceFrame(100, 2, 2, 5, 0),
		mkTraceFrame(200, 3, 3, 10, 0), // lap 3 starts
		mkTraceFrame(300, 3, 4, 13, 4), // +5
	)
	if len(spy.all) != 2 {
		t.Fatalf("persisted %d samples, want 2 (lap 3 only)", len(spy.all))
	}
	if a, b := spy.all[0], spy.all[1]; a.LapNumber != 3 || b.LapNumber != 3 || a.Distance != 0 || b.Distance != 5 {
		t.Errorf("samples = %+v, want lap 3 at distances 0, 5", spy.all)
	}
}

// batchSpy records every SaveBatch call so tests can assert flush behavior.
type batchSpy struct {
	batches int
	all     []Sample
}

func (b *batchSpy) SaveBatch(s []Sample) error            { b.batches++; b.all = append(b.all, s...); return nil }
func (b *batchSpy) ByLap(string, int32) ([]Sample, error) { return nil, nil }
func (b *batchSpy) DeleteSession(string) error            { return nil }

// TestTraceRecorder_BatchFlush: samples flush in batches (buffer cap) and nothing is lost across the
// cap flushes + the final session-end flush.
func TestTraceRecorder_BatchFlush(t *testing.T) {
	spy := &batchSpy{}
	r := NewTraceRecorder(spy)
	r.batch = 2 // small cap to force mid-lap flushes
	rec := &session.Record{ID: "s1"}
	r.OnSessionStart(rec)
	for i := 0; i < 5; i++ {
		r.OnFrame("s1", mkTraceFrame(i*100, 0, uint64(i+1), float32(i), 0))
	}
	r.OnSessionEnd(rec)

	if len(spy.all) != 5 {
		t.Errorf("persisted %d samples, want 5", len(spy.all))
	}
	if spy.batches < 2 {
		t.Errorf("batches = %d, want >= 2 (cap forced multiple flushes)", spy.batches)
	}
}

// mkChannelFrame builds a frame carrying the extra telemetry channels (fuel/tyre temps/clutch/boost)
// P2-1 threads into the trace, on top of the position/lap/seq the recorder always needs.
func mkChannelFrame() *telemetryv1.TelemetryFrame {
	f := mkTraceFrame(0, 0, 1, 0, 0)
	f.FuelCapacity = 100
	f.CurrentFuel = 50 // -> 50%
	f.TyreTempFl = 80
	f.TyreTempFr = 81
	f.TyreTempRl = 82
	f.TyreTempRr = 83
	f.Clutch = 0.5
	f.Boost = 1.5
	f.WheelSteeringAngleFrontLeft = 0.2
	f.WheelSteeringAngleFrontRight = 0.1
	return f
}

// TestTraceRecorder_Channels: the extra channels (fuel percent, tyre temps, clutch, boost) are copied
// from the frame into the persisted Sample.
func TestTraceRecorder_Channels(t *testing.T) {
	store := NewMemTraceStore()
	r := NewTraceRecorder(store)
	rec := &session.Record{ID: "s1"}
	r.OnSessionStart(rec)
	r.OnFrame("s1", mkChannelFrame())
	r.OnSessionEnd(rec)

	got, _ := store.ByLap("s1", 0)
	if len(got) != 1 {
		t.Fatalf("samples = %d, want 1", len(got))
	}
	s := got[0]
	if s.Fuel != 50 {
		t.Errorf("Fuel = %v, want 50 (50/100*100)", s.Fuel)
	}
	if s.TyreTempFL != 80 || s.TyreTempFR != 81 || s.TyreTempRL != 82 || s.TyreTempRR != 83 {
		t.Errorf("tyre temps = %v/%v/%v/%v, want 80/81/82/83", s.TyreTempFL, s.TyreTempFR, s.TyreTempRL, s.TyreTempRR)
	}
	if s.Clutch != 0.5 || s.Boost != 1.5 {
		t.Errorf("clutch/boost = %v/%v, want 0.5/1.5", s.Clutch, s.Boost)
	}
	if want := float32(0.2+0.1) / 2; s.Steering < want-1e-6 || s.Steering > want+1e-6 { // mean of the two front wheels, radians
		t.Errorf("Steering = %v, want %v (mean of FL/FR)", s.Steering, want)
	}
}

// TestTraceRecorder_FuelZeroCapacity: capacity <= 0 yields 0% (no division by zero), mirroring
// live.fuelPct.
func TestTraceRecorder_FuelZeroCapacity(t *testing.T) {
	store := NewMemTraceStore()
	r := NewTraceRecorder(store)
	rec := &session.Record{ID: "s1"}
	r.OnSessionStart(rec)
	f := mkTraceFrame(0, 0, 1, 0, 0)
	f.CurrentFuel = 10 // capacity 0 -> guard returns 0
	r.OnFrame("s1", f)
	r.OnSessionEnd(rec)

	got, _ := store.ByLap("s1", 0)
	if len(got) != 1 || got[0].Fuel != 0 {
		t.Errorf("Fuel = %v, want 0 (capacity<=0)", got)
	}
}

// TestTraceRecorder_DeltaFn: DeltaFn supplies the per-sample live delta-to-best. ok=true stores a
// pointer to the value; ok=false and a nil DeltaFn both leave DeltaToBestNs nil.
func TestTraceRecorder_DeltaFn(t *testing.T) {
	cases := []struct {
		name string
		fn   func() (int64, bool)
		want *int64
	}{
		{"ok", func() (int64, bool) { return 12345, true }, ptr(int64(12345))},
		{"not-ok", func() (int64, bool) { return 999, false }, nil},
		{"nil", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemTraceStore()
			r := NewTraceRecorder(store)
			r.DeltaFn = tc.fn
			rec := &session.Record{ID: "s1"}
			r.OnSessionStart(rec)
			r.OnFrame("s1", mkTraceFrame(0, 0, 1, 0, 0))
			r.OnSessionEnd(rec)

			got, _ := store.ByLap("s1", 0)
			if len(got) != 1 {
				t.Fatalf("samples = %d, want 1", len(got))
			}
			d := got[0].DeltaToBestNs
			switch {
			case tc.want == nil && d != nil:
				t.Errorf("DeltaToBestNs = %d, want nil", *d)
			case tc.want != nil && (d == nil || *d != *tc.want):
				t.Errorf("DeltaToBestNs = %v, want %d", d, *tc.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// TestTraceRecorder_DropsStrayFrames: frames before OnSessionStart or after OnSessionEnd are dropped
// (no state resurrection), mirroring the segmenter.
func TestTraceRecorder_DropsStrayFrames(t *testing.T) {
	store := NewMemTraceStore()
	r := NewTraceRecorder(store)
	r.OnFrame("ghost", mkTraceFrame(0, 0, 1, 0, 0)) // before any start
	rec := &session.Record{ID: "s1"}
	r.OnSessionStart(rec)
	r.OnFrame("s1", mkTraceFrame(0, 0, 1, 0, 0))
	r.OnSessionEnd(rec)
	r.OnFrame("s1", mkTraceFrame(100, 0, 2, 1, 0)) // after end

	s1, _ := store.ByLap("s1", 0)
	if len(s1) != 1 {
		t.Errorf("s1 lap0 samples = %d, want 1 (stray frames dropped)", len(s1))
	}
	ghost, _ := store.ByLap("ghost", 0)
	if len(ghost) != 0 {
		t.Errorf("ghost samples = %d, want 0", len(ghost))
	}
}
