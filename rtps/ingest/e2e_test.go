package ingest

import (
	"context"
	"net"
	"testing"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// TestIngest_EndToEnd exercises the whole seam over a real gRPC client stream (in-memory bufconn, no
// TCP): a fake collector streams two stints — one finished, one abandoned — and we assert the Ack
// frame count plus the two detected sessions in the store. This proves the transport + per-stream
// Detector wiring end to end, complementing the session package's golden-capture test.
func TestIngest_EndToEnd(t *testing.T) {
	lis := bufconn.Listen(1 << 20)
	store := session.NewMemStore()
	gs := grpc.NewServer()
	telemetryv1.RegisterTelemetryServer(gs, NewServer(store, session.NoopSink{}, session.Config{}, zap.NewNop()))
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Two stints: session 1 finishes (lap 2/2), session 2 is abandoned (lap 1/5), each bracketed by
	// menu frames so the debounce closes them.
	var frames []*telemetryv1.TelemetryFrame
	for i := range 12 {
		frames = append(frames, mkFrame(i*100, true, 1384, uint32(1+i), 2, 2))
	}
	frames = append(frames, mkFrame(1200, false, 1384, 100, -1, -1), mkFrame(1800, false, 1384, 101, -1, -1))
	for i := range 12 {
		frames = append(frames, mkFrame(2000+i*100, true, 2108, uint32(200+i), 1, 5))
	}
	frames = append(frames, mkFrame(3200, false, 2108, 300, -1, -1), mkFrame(3800, false, 2108, 301, -1, -1))

	stream, err := telemetryv1.NewTelemetryClient(conn).Ingest(context.Background())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	for _, f := range frames {
		if err := stream.Send(f); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	ack, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}

	if ack.GetFramesReceived() != uint64(len(frames)) {
		t.Errorf("Ack frames = %d, want %d", ack.GetFramesReceived(), len(frames))
	}
	got := store.List()
	if len(got) != 2 {
		t.Fatalf("store has %d sessions, want 2", len(got))
	}
	if got[0].Status != session.StatusFinished {
		t.Errorf("session 1 status = %q, want finished", got[0].Status)
	}
	if got[1].Status != session.StatusAbandoned {
		t.Errorf("session 2 status = %q, want abandoned", got[1].Status)
	}
}
