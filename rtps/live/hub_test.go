package live

import (
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func frameAt(seq uint64, lap int32, ts time.Time) *telemetryv1.TelemetryFrame {
	return &telemetryv1.TelemetryFrame{Sequence: seq, CurrentLap: lap, RecvTimestamp: timestamppb.New(ts)}
}

func TestHub_LatestAndSubscribe(t *testing.T) {
	h := New()
	if _, _, ok := h.Latest(); ok {
		t.Fatal("Latest ok=true before any Publish")
	}
	base := time.Unix(1700000000, 0)
	sub := h.Subscribe()
	defer sub.Close()

	h.Publish(frameAt(1, 3, base))
	f, lapStart, ok := h.Latest()
	if !ok || f.GetSequence() != 1 {
		t.Fatalf("Latest = %v ok=%v, want seq 1", f, ok)
	}
	if !lapStart.Equal(base) {
		t.Errorf("lapStart = %v, want %v", lapStart, base)
	}
	select {
	case <-sub.C():
		if got := sub.Take(); got.GetSequence() != 1 {
			t.Errorf("Take seq = %d, want 1", got.GetSequence())
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber not notified")
	}
}

func TestHub_Coalesces(t *testing.T) {
	h := New()
	sub := h.Subscribe()
	defer sub.Close()
	base := time.Unix(1700000000, 0)
	// three publishes, consumer never drained between them -> latest wins.
	h.Publish(frameAt(1, 1, base))
	h.Publish(frameAt(2, 1, base.Add(time.Second)))
	h.Publish(frameAt(3, 1, base.Add(2*time.Second)))
	<-sub.C()
	if got := sub.Take(); got.GetSequence() != 3 {
		t.Errorf("coalesced Take seq = %d, want 3 (freshest)", got.GetSequence())
	}
}

func TestHub_LapStartTracksLapChange(t *testing.T) {
	h := New()
	base := time.Unix(1700000000, 0)
	h.Publish(frameAt(1, 1, base))
	h.Publish(frameAt(2, 1, base.Add(time.Second)))
	h.Publish(frameAt(3, 2, base.Add(2*time.Second))) // lap 1 -> 2
	_, lapStart, _ := h.Latest()
	if !lapStart.Equal(base.Add(2 * time.Second)) {
		t.Errorf("lapStart = %v, want lap-2 start %v", lapStart, base.Add(2*time.Second))
	}
}
