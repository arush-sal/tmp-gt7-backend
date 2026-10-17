// Package ingest hosts the backend's telemetry seam: the gRPC server implementing the collector's
// client-streaming Telemetry.Ingest RPC. It is the entry point of the Session Management domain — it
// owns no racing logic, only the transport and the per-stream wiring to a session.Detector.
package ingest

import (
	"errors"
	"io"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// Server implements telemetryv1.TelemetryServer. It holds dependencies shared across all streams (the
// session Store, the Sink, detector Config, logger); each stream gets its own Detector so concurrent
// collectors never share detection state (the shared Store is the only cross-stream state, and it is
// goroutine-safe).
type Server struct {
	telemetryv1.UnimplementedTelemetryServer

	store session.Store
	sink  session.Sink
	cfg   session.Config
	log   *zap.Logger

	// Publisher, if set, receives each frame for live egress before Observe. nil = no live egress.
	Publisher Publisher
}

// Publisher receives every frame for real-time egress (the live.Hub). It is called on the ingest
// goroutine BEFORE detection/storage, so implementations must be non-blocking (publish-first, D-B25).
type Publisher interface {
	Publish(*telemetryv1.TelemetryFrame)
}

// NewServer wires a Server. A nil logger is replaced with a no-op so the server is always safe to call.
func NewServer(store session.Store, sink session.Sink, cfg session.Config, log *zap.Logger) *Server {
	if log == nil {
		log = zap.NewNop()
	}
	return &Server{store: store, sink: sink, cfg: cfg, log: log}
}

// Ingest implements the client-streaming RPC: the collector dials in and pushes TelemetryFrames until
// it closes the stream, at which point the server returns an Ack with the frame count.
//
// Each call gets a fresh Detector (one stream = one collector connection). Frames are fed to the
// Detector inline (it is pure and fast at 60Hz — no extra goroutine). The stream ending is the hard
// session-close signal: on a clean EOF the Detector is closed and the Ack sent; on any other error
// the Detector is still closed (so an in-progress session is force-finalized, not lost) and the error
// is returned to gRPC. Logging is per-stream only — never per frame (hot-path discipline).
func (s *Server) Ingest(stream telemetryv1.Telemetry_IngestServer) error {
	s.log.Info("ingest stream opened")
	frames, err := s.consume(stream.Recv)
	if errors.Is(err, io.EOF) {
		s.log.Info("ingest stream closed", zap.Uint64("frames", frames))
		return stream.SendAndClose(&telemetryv1.Ack{FramesReceived: frames})
	}
	s.log.Warn("ingest stream error; force-closed session", zap.Uint64("frames", frames), zap.Error(err))
	return err
}

// consume is the per-stream loop shared by the gRPC Ingest stream and the in-process Local publisher:
// a fresh Detector per stream, each frame published first (live egress) then observed, until recv
// returns an error. io.EOF is the clean end of stream; any error (clean or not) closes the Detector, so
// an in-progress session is always finalized, never lost. It returns the frame count and recv's error.
func (s *Server) consume(recv func() (*telemetryv1.TelemetryFrame, error)) (uint64, error) {
	d := session.NewDetector(s.sink, s.store, s.cfg)
	defer d.Close()
	var frames uint64
	for {
		f, err := recv()
		if err != nil {
			return frames, err
		}
		if s.Publisher != nil {
			s.Publisher.Publish(f) // egress first: consumers get the frame before any processing
		}
		d.Observe(f)
		frames++
	}
}
