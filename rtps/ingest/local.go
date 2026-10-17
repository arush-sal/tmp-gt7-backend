package ingest

import (
	"context"
	"io"
	"sync/atomic"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// Local is the in-process ingest path: the collector pipeline (live UDP or a replayed capture) running
// inside rtps publishes straight into it, with no network hop. It satisfies collector/ingest.Publisher
// structurally (Publish), and Run drains it through the same per-stream loop as the gRPC Ingest RPC,
// so an in-process stream behaves exactly like one collector connection.
//
// Publish is safe for exactly one producer goroutine (the receive or replay loop); Run is the single
// consumer.
type Local struct {
	srv *Server
	// ch is bounded and lossy (drop-oldest), mirroring collector/transport.Publisher: the producer is
	// the UDP receive goroutine, which must never block (a stalled recv loop means kernel UDP drops),
	// so when the consumer falls behind the oldest frame is evicted and counted instead.
	ch      chan *telemetryv1.TelemetryFrame
	dropped atomic.Uint64
	// done is closed when Run returns (consumer gone), so a blocked PublishWait can never wait on a
	// consumer that no longer exists. ch itself is never closed, so a send can never panic.
	done chan struct{}

	// Recording, if set before Run, is the user's Record switch, applied by this stream's Detector only.
	// External gRPC collector streams are not affected by it.
	Recording *session.RecordingControl
}

// NewLocal returns a Local feeding srv with a bufSize-deep buffer (the live_buffer.size config). A
// bufSize below 1 is raised to 1: an unbuffered channel would make Publish spin until Run receives.
func NewLocal(srv *Server, bufSize int) *Local {
	return &Local{srv: srv, ch: make(chan *telemetryv1.TelemetryFrame, max(bufSize, 1)), done: make(chan struct{})}
}

// Publish enqueues a frame without blocking. When the buffer is full it evicts the oldest frame
// (keeping the freshest telemetry) and increments the dropped counter. Single producer only.
func (l *Local) Publish(f *telemetryv1.TelemetryFrame) {
	for {
		select {
		case l.ch <- f:
			return
		default:
			select {
			case <-l.ch:
				l.dropped.Add(1)
			default:
			}
		}
	}
}

// PublishWait enqueues a frame, blocking while the buffer is full (never evicting). It returns
// ctx.Err() if ctx is cancelled first, or io.ErrClosedPipe once Run has returned. It is for the
// replay source: a replay has no kernel UDP buffer to protect, so losing frames would only make a
// replayed session differ from the original; fidelity matters more than latency. Live UDP must keep
// using the lossy Publish. Single producer only; do not mix with Publish on the same Local.
func (l *Local) PublishWait(ctx context.Context, f *telemetryv1.TelemetryFrame) error {
	select {
	case l.ch <- f:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-l.done:
		return io.ErrClosedPipe
	}
}

// Dropped returns the number of frames discarded because the buffer was full.
func (l *Local) Dropped() uint64 { return l.dropped.Load() }

// Run consumes published frames until ctx is cancelled, then ends the stream: the Detector is closed
// and any in-progress session finalized. It returns the number of frames ingested.
//
// On cancel, Run first drains whatever is still buffered before ending the stream, so the tail of a
// finished replay is not lost. Callers must stop the producer BEFORE cancelling ctx (shutdown order:
// collector/replay -> Local); a producer still publishing after cancel only adds frames to a stream
// that is about to end.
func (l *Local) Run(ctx context.Context) uint64 {
	defer close(l.done)
	l.srv.log.Info("in-process ingest stream opened")
	cfg := l.srv.cfg
	cfg.Recording = l.Recording
	frames, _ := l.srv.consume(func() (*telemetryv1.TelemetryFrame, error) {
		select {
		case f := <-l.ch:
			return f, nil
		case <-ctx.Done():
			select {
			case f := <-l.ch:
				return f, nil
			default:
				return nil, io.EOF
			}
		}
	}, cfg)
	l.srv.log.Info("in-process ingest stream closed", zap.Uint64("frames", frames), zap.Uint64("dropped", l.Dropped()))
	return frames
}
