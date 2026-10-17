package app

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/internal/config"
	"github.com/arush-sal/garage/backend/rtps/ingest"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// chanPub forwards every ingested frame on a channel (it runs on the Local's consumer goroutine).
type chanPub chan *telemetryv1.TelemetryFrame

func (c chanPub) Publish(f *telemetryv1.TelemetryFrame) { c <- f }

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

// collectorCfg is a valid in-process collector config on free loopback ports.
func collectorCfg(t *testing.T) config.BackendConfig {
	return config.BackendConfig{
		PS5IP:             "127.0.0.1",
		RecvPort:          freeUDPPort(t),
		SendPort:          freeUDPPort(t), // heartbeat target; nothing listens, harmless
		HeartbeatInterval: time.Hour,      // keep heartbeat quiet during the test
		LiveBufferSize:    64,
		RawBufferSize:     64,
		LogLevel:          "error",
	}
}

// TestCollector_InProcess runs the collector end to end in-process: real fixture packets over loopback
// UDP -> receiver -> pipeline -> Local -> the ingest Server's publish-first hook, then a clean shutdown.
// Packets are resent until delivered; dedup guarantees each distinct fixture is published once.
func TestCollector_InProcess(t *testing.T) {
	srv := ingest.NewServer(session.NewMemStore(), session.NoopSink{}, session.Config{}, zap.NewNop())
	frames := make(chanPub, 64)
	srv.Publisher = frames
	local := ingest.NewLocal(srv, 64)
	lctx, stopLocal := context.WithCancel(context.Background())
	defer stopLocal()
	go local.Run(lctx)

	cfg := collectorCfg(t)
	col, err := newCollector(cfg, zap.NewNop(), local)
	if err != nil {
		t.Fatalf("newCollector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- col.run(ctx, nil) }()

	sender, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: cfg.RecvPort})
	if err != nil {
		t.Fatalf("dial receiver: %v", err)
	}
	defer func() { _ = sender.Close() }()
	fixtures := []string{"packet_00.bin", "packet_01.bin", "packet_02.bin"}
	var payloads [][]byte
	for _, n := range fixtures {
		payloads = append(payloads, readFixture(t, n))
	}
	sendAll := func() {
		for _, p := range payloads {
			_, _ = sender.Write(p)
		}
	}

	got := map[uint32]*telemetryv1.TelemetryFrame{}
	sendAll()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(5 * time.Second)
	for len(got) < len(fixtures) {
		select {
		case f := <-frames:
			got[f.PackageId] = f
		case <-tick.C:
			sendAll()
		case <-deadline:
			t.Fatalf("received %d/%d frames", len(got), len(fixtures))
		}
	}
	for _, f := range got {
		if f.CarId != 1984 { // every fixture is the same car from the verified capture
			t.Errorf("frame %d: CarId = %d, want 1984", f.PackageId, f.CarId)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("collector run returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("collector run did not return after cancel")
	}
}

// TestNewCollector_BindError: a recv port already in use fails collector construction (and so rtps
// startup) instead of failing later on a background goroutine.
func TestNewCollector_BindError(t *testing.T) {
	busy, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	cfg := collectorCfg(t)
	cfg.RecvPort = busy.LocalAddr().(*net.UDPAddr).Port
	srv := ingest.NewServer(session.NewMemStore(), session.NoopSink{}, session.Config{}, zap.NewNop())
	if _, err := newCollector(cfg, zap.NewNop(), ingest.NewLocal(srv, 1)); err == nil {
		t.Fatal("newCollector on a busy port: want error, got nil")
	}
}
