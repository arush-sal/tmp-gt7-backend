package ingest

import (
	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// LoggingSink is a session.Sink that logs session lifecycle at info level — the Slice 1 way to see
// boundaries firing before the Processing domain exists. OnFrame is deliberately a no-op: it runs on
// the per-frame hot path, where logging is forbidden (same discipline as the collector's recv path).
type LoggingSink struct{ log *zap.Logger }

// NewLoggingSink returns a LoggingSink. A nil logger is replaced with a no-op.
func NewLoggingSink(log *zap.Logger) LoggingSink {
	if log == nil {
		log = zap.NewNop()
	}
	return LoggingSink{log: log}
}

// OnSessionStart logs a confirmed session opening.
func (s LoggingSink) OnSessionStart(r *session.Record) {
	s.log.Info("session start",
		zap.String("id", r.ID),
		zap.Int32("car_id", r.CarID),
		zap.Time("start", r.StartTime))
}

// OnFrame is a no-op (hot path — no logging).
func (s LoggingSink) OnFrame(string, *telemetryv1.TelemetryFrame) {}

// OnSessionEnd logs a closed session with its outcome and counts.
func (s LoggingSink) OnSessionEnd(r *session.Record) {
	s.log.Info("session end",
		zap.String("id", r.ID),
		zap.Int32("car_id", r.CarID),
		zap.String("status", string(r.Status)),
		zap.Uint64("frames", r.FrameCount),
		zap.Uint64("gaps", r.GapCount),
		zap.Duration("duration", r.EndTime.Sub(r.StartTime)))
}
