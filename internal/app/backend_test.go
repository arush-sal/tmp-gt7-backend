package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/internal/config"
	"go.uber.org/zap"
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
		done <- RunBackend(ctx, config.BackendConfig{IngestAddr: "127.0.0.1:0", APIAddr: "127.0.0.1:0", LogLevel: "error"}, zap.NewNop())
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
	err := RunBackend(context.Background(), config.BackendConfig{IngestAddr: "not-an-address:99999999", APIAddr: "127.0.0.1:0", LogLevel: "error"}, nil)
	if err == nil {
		t.Fatal("want bind error for invalid address, got nil")
	}
}
