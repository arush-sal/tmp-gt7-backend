package session

import (
	"testing"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
)

// countSink tallies the Sink calls it receives.
type countSink struct{ starts, frames, ends int }

func (c *countSink) OnSessionStart(*Record)                      { c.starts++ }
func (c *countSink) OnFrame(string, *telemetryv1.TelemetryFrame) { c.frames++ }
func (c *countSink) OnSessionEnd(*Record)                        { c.ends++ }

// TestMultiSink_FansOut verifies every wrapped sink receives every event.
func TestMultiSink_FansOut(t *testing.T) {
	a, b := &countSink{}, &countSink{}
	m := NewMultiSink(a, b)

	m.OnSessionStart(&Record{ID: "x"})
	m.OnFrame("x", &telemetryv1.TelemetryFrame{})
	m.OnFrame("x", &telemetryv1.TelemetryFrame{})
	m.OnSessionEnd(&Record{ID: "x"})

	for i, c := range []*countSink{a, b} {
		if c.starts != 1 || c.frames != 2 || c.ends != 1 {
			t.Errorf("sink %d = {starts:%d frames:%d ends:%d}, want {1 2 1}", i, c.starts, c.frames, c.ends)
		}
	}
}
