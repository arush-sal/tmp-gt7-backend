package session

import telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"

// Sink receives the Detector's lifecycle events. Session Management calls it; the Processing
// domain will implement it later to consume session-scoped telemetry (D-B5). Keeping this an
// interface (rather than a channel) keeps the cross-domain boundary explicit and trivially mockable
// in tests.
//
// Call order for a confirmed session is: exactly one OnSessionStart, zero or more OnFrame, exactly
// one OnSessionEnd. Sessions that never confirm (too short — e.g. the finish-line results flap) emit
// nothing at all.
type Sink interface {
	// OnSessionStart fires once when a session is confirmed (it has lasted at least MinSession).
	OnSessionStart(rec *Record)
	// OnFrame forwards one in-race frame belonging to the confirmed session identified by sessionID.
	OnFrame(sessionID string, f *telemetryv1.TelemetryFrame)
	// OnSessionEnd fires once when a confirmed session closes, carrying the finalized record
	// (EndTime + finished/abandoned status set).
	OnSessionEnd(rec *Record)
}

// NoopSink is a Sink that discards everything. Useful when only persistence (Store) is
// wanted, or as an embeddable base for sinks that care about a subset of the events.
type NoopSink struct{}

// OnSessionStart implements Sink; it discards the event.
func (NoopSink) OnSessionStart(*Record) {}

// OnFrame implements Sink; it discards the frame.
func (NoopSink) OnFrame(string, *telemetryv1.TelemetryFrame) {}

// OnSessionEnd implements Sink; it discards the event.
func (NoopSink) OnSessionEnd(*Record) {}
