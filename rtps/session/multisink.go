package session

import telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"

// MultiSink fans each Sink event out to all wrapped sinks, in registration order. It lets the ingest
// server drive several consumers from one stream — e.g. a logging sink plus the lap segmenter.
// Sinks are called synchronously and inline; a slow sink slows the stream, so keep them non-blocking
// (the same discipline as the Detector).
type MultiSink struct {
	sinks []Sink
}

// NewMultiSink wraps sinks into one Sink that fans out to each.
func NewMultiSink(sinks ...Sink) MultiSink {
	return MultiSink{sinks: sinks}
}

// OnSessionStart forwards to every sink.
func (m MultiSink) OnSessionStart(rec *Record) {
	for _, s := range m.sinks {
		s.OnSessionStart(rec)
	}
}

// OnFrame forwards to every sink.
func (m MultiSink) OnFrame(sessionID string, f *telemetryv1.TelemetryFrame) {
	for _, s := range m.sinks {
		s.OnFrame(sessionID, f)
	}
}

// OnSessionEnd forwards to every sink.
func (m MultiSink) OnSessionEnd(rec *Record) {
	for _, s := range m.sinks {
		s.OnSessionEnd(rec)
	}
}
