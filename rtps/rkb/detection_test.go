package rkb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// newDetectionTestDB writes a small normalized rkb.db exercising the three DetectionGeometry cases:
//   - "hsr": forward track, full world_bbox, boundary_points side='outline', one corner with apex_world.
//   - "cl1": null world_bbox columns, boundary_points side='cl' (must be preferred over any outline rows).
//   - "rev": is_reverse=1, boundary_points side='left'+'right' (no cl/outline), to exercise the
//     left+right-combined fallback and the reverse flag.
func newDetectionTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rkb.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	schema := `
create table tracks(slug text primary key,name text,ddm_id int,hex_id text,base int,
 is_reverse int,is_oval int,length_m real,num_corners_expected int,
 affine_world_bbox_min_x real,affine_world_bbox_min_z real,affine_world_bbox_max_x real,affine_world_bbox_max_z real,
 coordinate_usability text,physical_boundary_fidelity text,join_confidence text);
create table corners(slug text,turn int,apex_x real,apex_z real,radius_m real,direction text,type text,source text,
 apex_is_label int,corner_angle_deg real);
create table boundary_points(slug text,side text,idx int,world_x real,world_z real);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`insert into tracks(slug,name,base,is_reverse,affine_world_bbox_min_x,affine_world_bbox_min_z,
		  affine_world_bbox_max_x,affine_world_bbox_max_z,coordinate_usability,physical_boundary_fidelity,join_confidence)
		 values
		 ('hsr','High Speed Ring',20,0,-100.0,-200.0,100.0,200.0,'generated-coarse','derived','exact'),
		 ('cl1','Centerline One',NULL,0,NULL,NULL,NULL,NULL,'generated-coarse','derived','exact'),
		 ('rev','Reverse Track',20,1,NULL,NULL,NULL,NULL,'generated-coarse','derived','exact')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`insert into corners(slug,turn,apex_x,apex_z,direction,source) values('hsr',1,5.0,6.0,'left','outline_curvature')`); err != nil {
		t.Fatal(err)
	}
	// hsr: 20 outline points (idx 0..19) - subsampling at stride=8 must keep every 8th one: 0,8,16.
	for i := 0; i < 20; i++ {
		x := float64(i)
		z := float64(i) * 2
		if _, err := db.Exec(
			`insert into boundary_points(slug,side,idx,world_x,world_z) values('hsr','outline',?,?,?)`, i, x, z); err != nil {
			t.Fatal(err)
		}
	}
	// cl1: a stray outline row plus cl points - cl must win despite the outline row existing.
	if _, err := db.Exec(
		`insert into boundary_points(slug,side,idx,world_x,world_z) values('cl1','outline',0,999.0,999.0)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		if _, err := db.Exec(
			`insert into boundary_points(slug,side,idx,world_x,world_z) values('cl1','cl',?,?,?)`,
			i, float64(i), float64(i)*2); err != nil {
			t.Fatal(err)
		}
	}
	// rev: 8 left + 8 right points, no cl/outline. Combined (left then right) is 16 points; at
	// stride=8 that keeps combined[0] (left idx0) and combined[8] (right idx0) - proving both sides
	// are concatenated into one source, not just one of them picked.
	for i := 0; i < 8; i++ {
		if _, err := db.Exec(
			`insert into boundary_points(slug,side,idx,world_x,world_z) values
			 ('rev','left',?,?,?),('rev','right',?,?,?)`,
			i, 10.0+float64(i), 10.0+float64(i),
			i, 20.0+float64(i), 20.0+float64(i)); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestSQLiteStore_DetectionGeometry(t *testing.T) {
	st, err := Open(newDetectionTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	got, err := st.DetectionGeometry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	bySlug := make(map[string]TrackGeom, len(got))
	for _, g := range got {
		bySlug[g.Slug] = g
	}

	hsr, ok := bySlug["hsr"]
	if !ok {
		t.Fatal("hsr missing")
	}
	if hsr.IsReverse {
		t.Error("hsr should not be reverse")
	}
	if hsr.Base != 20 {
		t.Errorf("hsr base = %d, want 20", hsr.Base)
	}
	if !hsr.HasBbox {
		t.Error("hsr should have bbox")
	}
	wantBbox := [4]float64{-100.0, -200.0, 100.0, 200.0}
	if hsr.Bbox != wantBbox {
		t.Errorf("hsr bbox = %v, want %v", hsr.Bbox, wantBbox)
	}
	// 20 outline points at stride 8 -> idx 0,8,16 = 3 boundary points, plus 1 apex = 4.
	if len(hsr.Points) != 4 {
		t.Fatalf("hsr points = %d, want 4: %v", len(hsr.Points), hsr.Points)
	}
	if hsr.Points[0][0] != 0.0 || hsr.Points[1][0] != 8.0 || hsr.Points[2][0] != 16.0 {
		t.Errorf("hsr subsampled boundary points wrong: %v", hsr.Points[:3])
	}
	if hsr.Points[3][0] != 5.0 || hsr.Points[3][1] != 6.0 {
		t.Errorf("hsr apex point wrong: %v", hsr.Points[3])
	}

	cl1, ok := bySlug["cl1"]
	if !ok {
		t.Fatal("cl1 missing")
	}
	if cl1.HasBbox {
		t.Error("cl1 should have HasBbox=false (all-NULL bbox cols)")
	}
	if cl1.Base != 0 {
		t.Errorf("cl1 base = %d, want 0 (NULL base column)", cl1.Base)
	}
	// 9 cl points at stride 8 -> idx 0,8 kept = 2 points; must come from cl (x=0,8), never the
	// outline row (x=999).
	if len(cl1.Points) != 2 || cl1.Points[0][0] != 0.0 || cl1.Points[1][0] != 8.0 {
		t.Errorf("cl1 should use cl points (not outline): %v", cl1.Points)
	}

	rev, ok := bySlug["rev"]
	if !ok {
		t.Fatal("rev missing")
	}
	if !rev.IsReverse {
		t.Error("rev should be reverse")
	}
	if rev.Base != 20 {
		t.Errorf("rev base = %d, want 20 (shares hsr's forward/reverse pairing)", rev.Base)
	}
	// stride 8 over the 16-point (left++right) combined source keeps combined[0] (left idx0, x=10)
	// and combined[8] (right idx0, x=20) - proving both sides are concatenated, not just one chosen.
	if len(rev.Points) != 2 || rev.Points[0][0] != 10.0 || rev.Points[1][0] != 20.0 {
		t.Errorf("rev points wrong (want left+right combined): %v", rev.Points)
	}
}

// newDetectionFallbackTestDB writes a single-track db where the preferred source (cl) has rows but
// every one of them is null-coord, while outline (the lowest-priority source) has valid points. This
// is the regression case for the priority-vs-yield bug: naive "pick cl because it has rows" selection
// would return zero points even though a usable source exists.
func newDetectionFallbackTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rkb.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	schema := `
create table tracks(slug text primary key,name text,ddm_id int,hex_id text,base int,
 is_reverse int,is_oval int,length_m real,num_corners_expected int,
 affine_world_bbox_min_x real,affine_world_bbox_min_z real,affine_world_bbox_max_x real,affine_world_bbox_max_z real,
 coordinate_usability text,physical_boundary_fidelity text,join_confidence text);
create table corners(slug text,turn int,apex_x real,apex_z real,radius_m real,direction text,type text,source text,
 apex_is_label int,corner_angle_deg real);
create table boundary_points(slug text,side text,idx int,world_x real,world_z real);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`insert into tracks(slug,name,is_reverse,affine_world_bbox_min_x,affine_world_bbox_min_z,
		  affine_world_bbox_max_x,affine_world_bbox_max_z,coordinate_usability,physical_boundary_fidelity,join_confidence)
		 values('nullcl',NULL,0,NULL,NULL,NULL,NULL,'generated-coarse','derived','exact')`); err != nil {
		t.Fatal(err)
	}
	// cl: preferred source, but every row has a null coordinate - contributes zero valid points.
	for i := 0; i < 9; i++ {
		if _, err := db.Exec(
			`insert into boundary_points(slug,side,idx,world_x,world_z) values('nullcl','cl',?,NULL,NULL)`, i); err != nil {
			t.Fatal(err)
		}
	}
	// outline: lowest-priority source, but fully valid - must be the fallback that wins.
	for i := 0; i < 10; i++ {
		if _, err := db.Exec(
			`insert into boundary_points(slug,side,idx,world_x,world_z) values('nullcl','outline',?,?,?)`,
			i, float64(i)*3, float64(i)*4); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// TestSQLiteStore_DetectionGeometry_FallsBackWhenPreferredSourceAllNull is the regression guard for
// the priority-selection bug: cl has rows (so a naive "has rows -> pick it" check would choose it) but
// every coordinate is null, so it yields zero usable points after filtering. DetectionGeometry must
// fall through to outline rather than returning an empty point cloud for the track.
func TestSQLiteStore_DetectionGeometry_FallsBackWhenPreferredSourceAllNull(t *testing.T) {
	st, err := Open(newDetectionFallbackTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	got, err := st.DetectionGeometry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	g := got[0]
	if len(g.Points) == 0 {
		t.Fatal("nullcl points empty: preferred all-null source starved the track instead of falling back to outline")
	}
	// 10 outline points at stride 8 -> idx 0,8 kept = 2 points, all from outline (x = 3*idx).
	if len(g.Points) != 2 || g.Points[0][0] != 0.0 || g.Points[1][0] != 24.0 {
		t.Errorf("nullcl points should be the outline fallback: %v", g.Points)
	}
}

// TestSQLiteStore_DetectionGeometry_RealDB_NoEmptyPoints is the regression guard against the
// priority-vs-yield selection bug on the real generated corpus: skipped cleanly when tools/rkb/out is
// absent (same pattern as TestRoundTrip_AllTracks), otherwise asserts all 121 tracks are present.
// For tracks with coordinate_usability != 'needs-capture' (105 "generated" tier tracks), every one
// must have a non-empty Points slice. Tracks with coordinate_usability == 'needs-capture' (16 unresolved
// affine, undetectable-by-design tier) are exempt and may have empty Points - the frontend hides them.
// A non-needs-capture track with empty Points is a genuine boundary data gap that must be escalated.
func TestSQLiteStore_DetectionGeometry_RealDB_NoEmptyPoints(t *testing.T) {
	dbPath := filepath.Join("..", "..", "tools", "rkb", "out", "rkb.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skip("generated rkb.db not present; run `cd tools/rkb && ./.venv/bin/python -m rkb`")
	}
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	got, err := st.DetectionGeometry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 121 {
		t.Fatalf("len = %d, want 121", len(got))
	}

	// Load coordinate_usability for each track from the database.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	usability := make(map[string]string, len(got))
	rows, err := db.Query(`select slug, coordinate_usability from tracks`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var slug, coord string
		if err := rows.Scan(&slug, &coord); err != nil {
			t.Fatal(err)
		}
		usability[slug] = coord
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	// Check: non-needs-capture tracks must have non-empty Points; needs-capture are exempt.
	var empty []string
	for _, g := range got {
		coordUsability := usability[g.Slug]
		if coordUsability != "needs-capture" && len(g.Points) == 0 {
			empty = append(empty, g.Slug)
		}
	}
	if len(empty) > 0 {
		t.Errorf("%d generated tracks have empty Points (regression: usable coords but no boundary source or apex): %v", len(empty), empty)
	}
}
