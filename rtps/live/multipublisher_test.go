package live

import (
	"testing"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
)

// recordingPublisher is a fake framePublisher that records every frame it receives, in order.
type recordingPublisher struct {
	got []*telemetryv1.TelemetryFrame
}

func (r *recordingPublisher) Publish(f *telemetryv1.TelemetryFrame) {
	r.got = append(r.got, f)
}

func TestMultiPublisher_FansOutToAllInOrder(t *testing.T) {
	a := &recordingPublisher{}
	b := &recordingPublisher{}
	m := NewMultiPublisher(a, b)

	f1 := &telemetryv1.TelemetryFrame{CurrentLap: 1}
	f2 := &telemetryv1.TelemetryFrame{CurrentLap: 2}
	m.Publish(f1)
	m.Publish(f2)

	for name, r := range map[string]*recordingPublisher{"a": a, "b": b} {
		if len(r.got) != 2 || r.got[0] != f1 || r.got[1] != f2 {
			t.Fatalf("%s: got %v, want [f1 f2] in order", name, r.got)
		}
	}
}

func TestMultiPublisher_SkipsNilEntriesWithoutPanic(t *testing.T) {
	a := &recordingPublisher{}
	m := NewMultiPublisher(nil, a, nil)

	f := &telemetryv1.TelemetryFrame{CurrentLap: 7}
	m.Publish(f) // must not panic

	if len(a.got) != 1 || a.got[0] != f {
		t.Fatalf("got %v, want [f]", a.got)
	}
}
