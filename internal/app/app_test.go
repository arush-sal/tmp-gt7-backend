package app_test

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/internal/app"
	"github.com/arush-sal/garage/backend/internal/config"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// fakeRTPS is a minimal gRPC server standing in for the RTPS; it forwards received frames on a
// channel.
type fakeRTPS struct {
	telemetryv1.UnimplementedTelemetryServer
	frames chan *telemetryv1.TelemetryFrame
}

func (s *fakeRTPS) Ingest(stream telemetryv1.Telemetry_IngestServer) error {
	for {
		f, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&telemetryv1.Ack{})
		}
		if err != nil {
			return err
		}
		s.frames <- f
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	p := c.LocalAddr().(*net.UDPAddr).Port
	_ = c.Close()
	return p
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "collector", "protocol", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// TestAppSmoke runs the whole collector end to end: a fake RTPS on localhost, real fixture packets
// replayed over loopback UDP into the receiver, and a clean shutdown on cancel. Because packets are
// resent until delivered, dedup guarantees each distinct fixture is published exactly once.
func TestAppSmoke(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	fake := &fakeRTPS{frames: make(chan *telemetryv1.TelemetryFrame, 32)}
	telemetryv1.RegisterTelemetryServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	recvPort := freeUDPPort(t)
	cfg := config.Config{
		PS5IP:             "127.0.0.1",
		RecvPort:          recvPort,
		SendPort:          freeUDPPort(t), // heartbeat target; nothing listens, harmless
		RTPSAddr:          lis.Addr().String(),
		HeartbeatInterval: time.Hour, // keep heartbeat quiet during the test
		LiveBufferSize:    64,
		LogLevel:          "error",
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, cfg, zap.NewNop(), nil) }()

	sender, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: recvPort})
	if err != nil {
		t.Fatalf("dial receiver: %v", err)
	}
	defer func() { _ = sender.Close() }()

	fixtures := []string{"packet_00.bin", "packet_01.bin", "packet_02.bin"}
	payloads := make([][]byte, len(fixtures))
	for i, n := range fixtures {
		payloads[i] = readFixture(t, n)
	}
	sendAll := func() {
		for _, p := range payloads {
			_, _ = sender.Write(p)
		}
	}

	// Resend until the RTPS has all three distinct frames (or time out). Dedup drops the resends.
	got := map[uint32]*telemetryv1.TelemetryFrame{}
	sendAll()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(5 * time.Second)
	for len(got) < len(fixtures) {
		select {
		case f := <-fake.frames:
			got[f.PackageId] = f
		case <-tick.C:
			sendAll()
		case <-deadline:
			t.Fatalf("received %d/%d frames", len(got), len(fixtures))
		}
	}

	// Every frame is the same car from the verified capture.
	for _, f := range got {
		if f.CarId != 1984 {
			t.Errorf("frame %d: CarId = %d, want 1984", f.PackageId, f.CarId)
		}
	}

	// Clean shutdown: cancel and Run must return nil (context-cancel is not an error).
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("app.Run returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("app.Run did not return after cancel")
	}
}
