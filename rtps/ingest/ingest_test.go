package ingest

import (
	"errors"
	"io"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeStream implements grpc.ClientStreamingServer[TelemetryFrame, Ack] without real gRPC transport:
// Recv replays a scripted frame slice (then a terminal error), and SendAndClose captures the Ack. The
// embedded grpc.ServerStream is nil — the Ingest handler only calls Recv and SendAndClose, so the
// other ServerStream methods are never invoked.
type fakeStream struct {
	grpc.ServerStream
	frames  []*telemetryv1.TelemetryFrame
	i       int
	recvErr error // returned once the scripted frames are exhausted (default io.EOF)
	ack     *telemetryv1.Ack
}

func (f *fakeStream) Recv() (*telemetryv1.TelemetryFrame, error) {
	if f.i >= len(f.frames) {
		if f.recvErr != nil {
			return nil, f.recvErr
		}
		return nil, io.EOF
	}
	fr := f.frames[f.i]
	f.i++
	return fr, nil
}

func (f *fakeStream) SendAndClose(a *telemetryv1.Ack) error {
	f.ack = a
	return nil
}

// mkFrame builds an in/out-of-race frame at base+ms with the fields the Detector reads.
func mkFrame(ms int, inRace bool, car int32, pkg uint32, curLap, totLaps int32) *telemetryv1.TelemetryFrame {
	base := time.Unix(1_700_000_000, 0)
	return &telemetryv1.TelemetryFrame{
		RecvTimestamp: timestamppb.New(base.Add(time.Duration(ms) * time.Millisecond)),
		InRace:        inRace,
		CarId:         car,
		PackageId:     pkg,
		CurrentLap:    curLap,
		TotalLaps:     totLaps,
		Source:        telemetryv1.Source_SOURCE_LIVE,
	}
}

// oneRaceStream is a minimal in-race stint (> MinSession) followed by menu frames that close it.
func oneRaceStream() []*telemetryv1.TelemetryFrame {
	var fs []*telemetryv1.TelemetryFrame
	for i := range 12 { // 0..1100ms in-race, lap 2/2 => finished
		fs = append(fs, mkFrame(i*100, true, 1384, uint32(100+i), 2, 2))
	}
	fs = append(fs, mkFrame(1200, false, 1384, 112, -1, -1), mkFrame(1800, false, 1384, 113, -1, -1))
	return fs
}

// TestServer_IngestEOF: a normal client-closed stream is acked with the exact frame count, and the
// detected session lands in the store.
func TestServer_IngestEOF(t *testing.T) {
	store := session.NewMemStore()
	srv := NewServer(store, session.NoopSink{}, session.Config{}, zap.NewNop())

	frames := oneRaceStream()
	fs := &fakeStream{frames: frames}
	if err := srv.Ingest(fs); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	if fs.ack == nil || fs.ack.GetFramesReceived() != uint64(len(frames)) {
		t.Fatalf("Ack frames = %v, want %d", fs.ack, len(frames))
	}
	if n := len(store.List()); n != 1 {
		t.Errorf("store has %d sessions, want 1", n)
	}
	if got := store.List(); len(got) == 1 && got[0].Status != session.StatusFinished {
		t.Errorf("session status = %q, want finished", got[0].Status)
	}
}

// TestServer_IngestErrorForceCloses: a mid-stream transport error is propagated, and the in-progress
// session is still finalized (force-closed) rather than lost.
func TestServer_IngestErrorForceCloses(t *testing.T) {
	store := session.NewMemStore()
	srv := NewServer(store, session.NoopSink{}, session.Config{}, zap.NewNop())

	// A confirmed (> MinSession) in-race stint, then the stream errors before any menu frame.
	var frames []*telemetryv1.TelemetryFrame
	for i := range 12 {
		frames = append(frames, mkFrame(i*100, true, 1384, uint32(1+i), 2, 2))
	}
	boom := errors.New("connection reset")
	fs := &fakeStream{frames: frames, recvErr: boom}

	err := srv.Ingest(fs)
	if !errors.Is(err, boom) {
		t.Fatalf("Ingest err = %v, want %v", err, boom)
	}
	if n := len(store.List()); n != 1 {
		t.Errorf("store has %d sessions after error, want 1 (force-closed)", n)
	}
}
