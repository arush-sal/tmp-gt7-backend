package app

import (
	"context"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/internal/config"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	_ "modernc.org/sqlite"
)

// withHermeticRKB points $HOME at a fresh t.TempDir() and writes a minimal but valid RKB SQLite DB at
// $HOME/.garage/rkb.db, so RunBackend's fixed-path rkb.Open (mode=ro&immutable=1) has something to
// open+ping. RunBackend resolves the path via os.UserHomeDir(), which honors $HOME on Linux. Without
// this, TestRunBackend_* only pass on a machine that happens to already have ~/.garage/rkb.db - a
// hermeticity regression introduced when the fixed-path RKB load was added.
func withHermeticRKB(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	garageDir := filepath.Join(home, ".garage")
	if err := os.MkdirAll(garageDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", garageDir, err)
	}
	dbPath := filepath.Join(garageDir, "rkb.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer func() { _ = db.Close() }()

	// Minimal subset of the RKB schema (rtps/rkb/sqlite_test.go's newTestDBAt is the reference): no
	// rows needed, rkb.Open only opens+pings the file. All 7 tables are created anyway so the file
	// matches the real schema shape rather than relying on which table(s) Ping happens to touch.
	const schema = `
create table tracks(slug text primary key,name text,ddm_id int,hex_id text,base int,
 is_reverse int,is_oval int,length_m real,num_corners_expected int,
 width_m real,width_min_m real,width_max_m real,width_source text,width_n_total int,width_n_kept int,
 affine_scale real,affine_rot_deg real,affine_tx real,affine_tz real,affine_source text,
 affine_rms_m real,affine_sf_anchor_err_m real,affine_scale_sanity_pct real,
 affine_world_bbox_min_x real,affine_world_bbox_min_z real,affine_world_bbox_max_x real,affine_world_bbox_max_z real,
 affine_svg_bbox_min_x real,affine_svg_bbox_min_y real,affine_svg_bbox_max_x real,affine_svg_bbox_max_y real,
 boundary_source text,coordinate_usability text,physical_boundary_fidelity text,join_confidence text,
 provenance_affine text,provenance_corners text,provenance_boundary text,provenance_width text);
create table corners(slug text,turn int,apex_x real,apex_z real,radius_m real,direction text,type text,source text,
 apex_is_label int,corner_angle_deg real,apex_speed_kmh real,lat_accel_g real,gear_at_apex int,banked int,
 banking_deg_at_apex real,length_m real);
create table corner_phases(slug text,turn int,phase text,world_x real,world_z real,phase_distance_from_apex real);
create table sectors(slug text,sector text,turns text,label_x real,label_z real);
create table boundary_points(slug text,side text,idx int,world_x real,world_z real);
create table width_profile(slug text,idx int,width_m real);
create table reference_times(slug text,sector text,car_id int,car_category text,performance_point real,reference_time_ms int);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema in %s: %v", dbPath, err)
	}
}

// TestRunBackend_GracefulStop verifies the lifecycle: RunBackend binds and serves, and returns nil
// (no error) once its context is cancelled — the SIGINT/SIGTERM path. Port :0 lets the OS pick a free
// port so the test never collides with a real service.
func TestRunBackend_GracefulStop(t *testing.T) {
	withHermeticRKB(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunBackend(ctx, config.BackendConfig{IngestAddr: "127.0.0.1:0", APIAddr: "127.0.0.1:0", LogLevel: "error"}, zap.NewNop(), nil)
	}()

	time.Sleep(100 * time.Millisecond) // let it bind + start serving
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunBackend returned error on graceful stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBackend did not stop within 2s of context cancel")
	}
}

// TestRunBackend_BindError surfaces a listen failure (a malformed address never binds). It also
// passes a nil logger to exercise the nil-logger guard (must not panic).
func TestRunBackend_BindError(t *testing.T) {
	withHermeticRKB(t)
	err := RunBackend(context.Background(), config.BackendConfig{IngestAddr: "not-an-address:99999999", APIAddr: "127.0.0.1:0", LogLevel: "error"}, nil, nil)
	if err == nil {
		t.Fatal("want bind error for invalid address, got nil")
	}
}

// TestRunBackend_CollectorBindFails: with --ps5.ip set, a busy UDP recv port fails rtps startup with a
// clear error rather than serving without telemetry.
func TestRunBackend_CollectorBindFails(t *testing.T) {
	withHermeticRKB(t)
	busy, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	cfg := collectorCfg(t)
	cfg.RecvPort = busy.LocalAddr().(*net.UDPAddr).Port
	cfg.IngestAddr, cfg.APIAddr = "127.0.0.1:0", "127.0.0.1:0"

	errc := make(chan error, 1)
	go func() { errc <- RunBackend(context.Background(), cfg, zap.NewNop(), nil) }()
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "collector") {
			t.Fatalf("RunBackend err = %v, want a collector startup error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunBackend kept running despite the collector failing to bind")
	}
}

// TestRunBackend_ReplayOpenFails: an unreadable --replay-file fails startup.
func TestRunBackend_ReplayOpenFails(t *testing.T) {
	withHermeticRKB(t)
	cfg := config.BackendConfig{
		IngestAddr: "127.0.0.1:0", APIAddr: "127.0.0.1:0", LogLevel: "error", LiveBufferSize: 8,
		ReplayFile: filepath.Join(t.TempDir(), "missing.pcapng"), ReplaySpeed: 1, RecvPort: 33740,
	}
	if err := RunBackend(context.Background(), cfg, zap.NewNop(), nil); err == nil {
		t.Fatal("RunBackend with a missing replay file: want error, got nil")
	}
}

// writeFixtureCapture writes the GT7 fixture packets as UDP datagrams to dstPort into a pcapng.
func writeFixtureCapture(t *testing.T, path string, dstPort int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	w, err := pcapgo.NewNgWriter(f, layers.LinkTypeEthernet)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range []string{"packet_00.bin", "packet_01.bin", "packet_02.bin"} {
		ip := &layers.IPv4{Version: 4, IHL: 5, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: net.IP{10, 0, 0, 1}, DstIP: net.IP{10, 0, 0, 2}}
		udp := &layers.UDP{SrcPort: 40000, DstPort: layers.UDPPort(dstPort)}
		if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
			t.Fatal(err)
		}
		eth := &layers.Ethernet{SrcMAC: net.HardwareAddr{1, 1, 1, 1, 1, 1}, DstMAC: net.HardwareAddr{2, 2, 2, 2, 2, 2}, EthernetType: layers.EthernetTypeIPv4}
		buf := gopacket.NewSerializeBuffer()
		if err := gopacket.SerializeLayers(buf, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, ip, udp, gopacket.Payload(readFixture(t, n))); err != nil {
			t.Fatal(err)
		}
		ci := gopacket.CaptureInfo{Timestamp: time.Unix(1_700_000_000, 0).Add(time.Duration(i) * 16 * time.Millisecond), CaptureLength: len(buf.Bytes()), Length: len(buf.Bytes())}
		if err := w.WritePacket(ci, buf.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
}

// TestRunBackend_ReplayFinishesAndKeepsServing: a replay runs to EOF, its stream is finalized (the
// in-process ingest closes with every published frame), and rtps keeps serving until cancelled.
func TestRunBackend_ReplayFinishesAndKeepsServing(t *testing.T) {
	withHermeticRKB(t)
	path := filepath.Join(t.TempDir(), "fx.pcapng")
	writeFixtureCapture(t, path, 33740)
	cfg := config.BackendConfig{
		IngestAddr: "127.0.0.1:0", APIAddr: "127.0.0.1:0", LogLevel: "info", LiveBufferSize: 64,
		ReplayFile: path, ReplaySpeed: 1, RecvPort: 33740,
	}
	core, logs := observer.New(zap.InfoLevel)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunBackend(ctx, cfg, zap.New(core), nil) }()

	deadline := time.Now().Add(3 * time.Second)
	for logs.FilterMessage("replay finished").Len() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("replay never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	closed := logs.FilterMessage("in-process ingest stream closed").All()
	if len(closed) != 1 || closed[0].ContextMap()["frames"] != uint64(3) {
		t.Fatalf("in-process stream close logs = %v, want one with frames=3", closed)
	}
	select {
	case err := <-done:
		t.Fatalf("RunBackend returned after replay EOF (%v); it must keep serving", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunBackend returned error on graceful stop: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunBackend did not stop after cancel")
	}
}
