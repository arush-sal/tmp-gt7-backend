package rkb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// newTestDB writes a tiny normalized rkb.db to a temp dir and returns its path.
func newTestDB(t *testing.T) string {
	t.Helper()
	return newTestDBAt(t, filepath.Join(t.TempDir(), "rkb.db"))
}

// newTestDBAt writes the same tiny normalized rkb.db to an explicit path and returns it, so callers
// can exercise paths with special characters (see TestSQLiteStore_Open_PathWithSpecialChars).
func newTestDBAt(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// Schema mirrors the generator's current normalized layout (tools/rkb writers): tracks carries the
	// width_n_total/width_n_kept measurement counts, and width_profile is its own ordered table.
	schema := `
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
		t.Fatal(err)
	}
	// one reliable track (hsr) and one needs-capture track (bar)
	if _, err := db.Exec(
		`insert into tracks(slug,name,ddm_id,hex_id,length_m,num_corners_expected,affine_source,boundary_source,
		 coordinate_usability,physical_boundary_fidelity,join_confidence,width_source,width_n_total,width_n_kept) values
		 ('hsr','High Speed Ring',11,NULL,4345.0,6,'src','outline','generated-coarse','derived','exact','svg_stroke',3,3),
		 ('bar','Bar',NULL,NULL,NULL,NULL,'src','outline','needs-capture','none','fuzzy','svg_stroke',NULL,NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`insert into corners(slug,turn,apex_x,apex_z,radius_m,direction,type,source,apex_is_label,corner_angle_deg)
		 values('hsr',1,10.0,20.0,NULL,'left',NULL,'outline_curvature',0,-29.6)`); err != nil {
		t.Fatal(err)
	}
	// A corner_phases row exists for hsr turn 1, but the corner's source is outline_curvature, so Track
	// MUST NOT emit a phases key (phases are only reconstructed for kinematic/derived-reverse corners).
	if _, err := db.Exec(
		`insert into corner_phases(slug,turn,phase,world_x,world_z) values('hsr',1,'apex',10.0,20.0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`insert into sectors(slug,sector,turns,label_x,label_z) values('hsr','S1','1,2',NULL,NULL)`); err != nil {
		t.Fatal(err)
	}
	// hsr is an SVG (outline) track: an outline side plus a stray cl row to exercise loadBoundary's
	// side dispatch. idx ordering is asserted in TestSQLiteStore_Track.
	if _, err := db.Exec(
		`insert into boundary_points(slug,side,idx,world_x,world_z) values
		 ('hsr','outline',0,0.0,0.0),('hsr','cl',1,3.0,4.0),('hsr','cl',0,1.0,2.0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`insert into width_profile(slug,idx,width_m) values('hsr',0,9.6),('hsr',1,9.8),('hsr',2,9.7)`); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSQLiteStore_Index(t *testing.T) {
	st, err := Open(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	got, err := st.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Slug != "bar" || got[1].Slug != "hsr" { // slug order
		t.Errorf("order = %s,%s; want bar,hsr", got[0].Slug, got[1].Slug)
	}
	if !got[0].NeedsCapture {
		t.Error("bar should be needs_capture")
	}
	if got[1].NeedsCapture {
		t.Error("hsr should not be needs_capture")
	}
	if got[1].HexID != nil {
		t.Errorf("hsr HexID = %v, want nil", *got[1].HexID)
	}
}

func TestSQLiteStore_Track(t *testing.T) {
	st, err := Open(newTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()

	if _, found, _ := st.Track(ctx, "nope"); found {
		t.Error("unknown slug should be found=false")
	}
	rec, found, err := st.Track(ctx, "hsr")
	if err != nil || !found {
		t.Fatalf("hsr: found=%v err=%v", found, err)
	}
	if rec.Name != "High Speed Ring" || rec.HexID != nil {
		t.Errorf("scalars wrong: %+v", rec)
	}
	if len(rec.Corners) != 1 || rec.Corners[0].Turn != 1 {
		t.Fatalf("corner count/turn wrong: %+v", rec.Corners)
	}
	c := rec.Corners[0]
	if len(c.ApexWorld) != 2 || c.ApexWorld[0] == nil || c.ApexWorld[1] == nil ||
		*c.ApexWorld[0] != 10.0 || *c.ApexWorld[1] != 20.0 {
		t.Errorf("apex_world wrong: %+v", c.ApexWorld)
	}
	if c.Direction == nil || *c.Direction != "left" {
		t.Error("direction should be 'left'")
	}
	if c.RadiusM != nil {
		t.Error("radius should be nil")
	}
	// outline_curvature corner: phases key must be omitted despite a stray corner_phases row.
	if c.Phases != nil {
		t.Errorf("phases should be nil (omitted) for outline_curvature, got %s", c.Phases)
	}
	if len(rec.Sectors) != 1 || len(rec.Sectors[0].Turns) != 2 || rec.Sectors[0].Turns[1] != 2 {
		t.Errorf("sector turns wrong: %+v", rec.Sectors)
	}
	if rec.Sectors[0].LabelWorld != nil {
		t.Error("label_world should be nil when coords are NULL")
	}
	if len(rec.Boundary.Outline) != 1 {
		t.Errorf("outline wrong: %+v", rec.Boundary.Outline)
	}
	// cl rows are inserted out of idx order; loadBoundary must return them idx-ordered.
	if len(rec.Boundary.Cl) != 2 || rec.Boundary.Cl[0][0] == nil || rec.Boundary.Cl[1][0] == nil ||
		*rec.Boundary.Cl[0][0] != 1.0 || *rec.Boundary.Cl[1][0] != 3.0 {
		t.Errorf("cl wrong/unordered: %+v", rec.Boundary.Cl)
	}
	if rec.Width.NTotal == nil || *rec.Width.NTotal != 3 {
		t.Errorf("width n_total wrong: %+v", rec.Width.NTotal)
	}
	if rec.Width.Profile == nil || len(*rec.Width.Profile) != 3 || (*rec.Width.Profile)[1] != 9.8 {
		t.Errorf("width profile wrong: %+v", rec.Width.Profile)
	}
}

// TestSQLiteStore_Open_PathWithSpecialChars guards against the DSN construction regressing to a
// fmt.Sprintf/query-string concatenation: a '#' in the path introduces a URI fragment that would
// swallow "?mode=ro&immutable=1", silently opening the DB writable.
func TestSQLiteStore_Open_PathWithSpecialChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a#b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := newTestDBAt(t, filepath.Join(dir, "rkb.db"))

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	got, err := st.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Slug != "bar" || got[1].Slug != "hsr" {
		t.Errorf("order = %s,%s; want bar,hsr", got[0].Slug, got[1].Slug)
	}
}
