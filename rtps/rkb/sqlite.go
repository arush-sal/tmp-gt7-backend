package rkb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // pure-Go driver, registered as "sqlite"
)

// SQLiteStore serves the RKB from a read-only SQLite file (the offline-generated rkb.db).
type SQLiteStore struct {
	db *sql.DB
}

// Open opens the rkb.db at path read-only (immutable - the generator is the only writer) and pings
// it so a missing/corrupt file fails loudly here, not on the first request.
func Open(path string) (*SQLiteStore, error) {
	// Build the DSN via net/url rather than fmt.Sprintf: a raw path containing '#', '?', or spaces
	// would otherwise bleed into (or truncate) the query string, silently dropping mode=ro and
	// opening the DB writable - a hard requirement violation. Opaque + EscapedPath keeps the path
	// exact while RawQuery guarantees mode=ro/immutable=1 always apply.
	u := url.URL{Scheme: "file", Opaque: (&url.URL{Path: path}).EscapedPath()}
	u.RawQuery = url.Values{"mode": {"ro"}, "immutable": {"1"}}.Encode()
	dsn := u.String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("rkb: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("rkb: ping %s: %w", path, err)
	}
	return &SQLiteStore{db: db}, nil
}

// Close closes the underlying handle.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// Index returns one entry per track in slug order.
func (s *SQLiteStore) Index(ctx context.Context) ([]IndexEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`select slug,name,ddm_id,hex_id,length_m,num_corners_expected,
		        coordinate_usability,physical_boundary_fidelity
		 from tracks order by slug`)
	if err != nil {
		return nil, fmt.Errorf("rkb: index: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IndexEntry
	for rows.Next() {
		var e IndexEntry
		if err := rows.Scan(&e.Slug, &e.Name, &e.DDMID, &e.HexID, &e.LengthM,
			&e.NumCornersExpected, &e.CoordinateUsability, &e.PhysicalBoundaryFidelity); err != nil {
			return nil, fmt.Errorf("rkb: index scan: %w", err)
		}
		e.NeedsCapture = e.CoordinateUsability == "needs-capture"
		out = append(out, e)
	}
	return out, rows.Err()
}

// Track assembles the full canonical record for slug from all normalized tables. Returns found=false
// for an unknown slug. bbox arrays are nil (JSON null) unless all four scalar columns are present.
func (s *SQLiteStore) Track(ctx context.Context, slug string) (*Record, bool, error) {
	rec := &Record{}
	var a Affine
	var wbMinX, wbMinZ, wbMaxX, wbMaxZ sql.NullFloat64
	var sbMinX, sbMinY, sbMaxX, sbMaxY sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`select slug,name,ddm_id,hex_id,base,is_reverse,is_oval,length_m,num_corners_expected,
		        affine_scale,affine_rot_deg,affine_tx,affine_tz,affine_source,
		        affine_rms_m,affine_sf_anchor_err_m,affine_scale_sanity_pct,
		        affine_world_bbox_min_x,affine_world_bbox_min_z,affine_world_bbox_max_x,affine_world_bbox_max_z,
		        affine_svg_bbox_min_x,affine_svg_bbox_min_y,affine_svg_bbox_max_x,affine_svg_bbox_max_y,
		        boundary_source,width_m,width_min_m,width_max_m,width_source,width_n_total,width_n_kept,
		        coordinate_usability,physical_boundary_fidelity,join_confidence,
		        provenance_affine,provenance_corners,provenance_boundary,provenance_width
		 from tracks where slug=?`, slug).Scan(
		&rec.Slug, &rec.Name, &rec.DDMID, &rec.HexID, &rec.Base, &rec.IsReverse, &rec.IsOval,
		&rec.LengthM, &rec.NumCornersExpected,
		&a.Scale, &a.RotDeg, &a.Tx, &a.Tz, &a.Source, &a.RmsM, &a.SfAnchorErrM, &a.ScaleSanityPct,
		&wbMinX, &wbMinZ, &wbMaxX, &wbMaxZ, &sbMinX, &sbMinY, &sbMaxX, &sbMaxY,
		&rec.Boundary.Source, &rec.Width.WidthM, &rec.Width.MinM, &rec.Width.MaxM, &rec.Width.Source,
		&rec.Width.NTotal, &rec.Width.NKept,
		&rec.Confidence.CoordinateUsability, &rec.Confidence.PhysicalBoundaryFidelity, &rec.Confidence.Join,
		&rec.Provenance.Affine, &rec.Provenance.Corners, &rec.Provenance.Boundary, &rec.Provenance.Width,
	)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("rkb: track %s: %w", slug, err)
	}
	a.WorldBbox = bbox4(wbMinX, wbMinZ, wbMaxX, wbMaxZ)
	a.SvgBbox = bbox4(sbMinX, sbMinY, sbMaxX, sbMaxY)
	rec.Affine = a

	if err := s.loadWidth(ctx, rec); err != nil {
		return nil, false, err
	}
	if err := s.loadCorners(ctx, rec); err != nil {
		return nil, false, err
	}
	if err := s.loadSectors(ctx, rec); err != nil {
		return nil, false, err
	}
	if err := s.loadBoundary(ctx, rec); err != nil {
		return nil, false, err
	}
	return rec, true, nil
}

// bbox4 returns a *[minx,minz,maxx,maxz] slice, or nil (JSON null) when any component is NULL. The
// generator writes all four together, so a partial bbox never occurs, but nil-on-any keeps the
// reconstruction total.
func bbox4(a, b, c, d sql.NullFloat64) *[]float64 {
	if !a.Valid || !b.Valid || !c.Valid || !d.Valid {
		return nil
	}
	v := []float64{a.Float64, b.Float64, c.Float64, d.Float64}
	return &v
}

// nullF converts a nullable REAL to a *float64 (nil for NULL) for the [x,z] apex/label arrays whose
// elements are individually nullable.
func nullF(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

// loadWidth reads the measured cross-section series (measured/victorydash tracks) ordered by idx into
// Width.Profile. Profile stays nil (JSON null) for SVG tracks, which have no width_profile rows.
func (s *SQLiteStore) loadWidth(ctx context.Context, rec *Record) error {
	rows, err := s.db.QueryContext(ctx,
		`select width_m from width_profile where slug=? order by idx`, rec.Slug)
	if err != nil {
		return fmt.Errorf("rkb: width_profile %s: %w", rec.Slug, err)
	}
	defer func() { _ = rows.Close() }()
	var prof []float64
	for rows.Next() {
		var w float64
		if err := rows.Scan(&w); err != nil {
			return fmt.Errorf("rkb: width_profile scan: %w", err)
		}
		prof = append(prof, w)
	}
	if prof != nil {
		rec.Width.Profile = &prof
	}
	return rows.Err()
}

func (s *SQLiteStore) loadCorners(ctx context.Context, rec *Record) error {
	// Load ALL corner_phases rows for the slug up front, before opening the corners query below. The
	// corners loop used to run a per-corner corner_phases query while the corners *sql.Rows was still
	// open; with SetMaxOpenConns(4) capping the pool, concurrent Track() calls could each hold an outer
	// Rows connection while contending for a second connection for the inner query, exhausting the pool
	// and deadlocking. Fetching everything in one query eliminates the overlapping open Rows entirely.
	phasesByTurn, err := s.loadPhasesByTurn(ctx, rec.Slug)
	if err != nil {
		return err
	}

	rows, err := s.db.QueryContext(ctx,
		`select turn,apex_x,apex_z,radius_m,direction,type,source,apex_is_label,corner_angle_deg,
		        apex_speed_kmh,lat_accel_g,gear_at_apex,banked,banking_deg_at_apex,length_m
		 from corners where slug=? order by turn`, rec.Slug)
	if err != nil {
		return fmt.Errorf("rkb: corners %s: %w", rec.Slug, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var c Corner
		var ax, az sql.NullFloat64
		var apexIsLabel, banked sql.NullBool
		if err := rows.Scan(&c.Turn, &ax, &az, &c.RadiusM, &c.Direction, &c.Type, &c.Source,
			&apexIsLabel, &c.HeadingChangeDeg, &c.ApexSpeedKmh, &c.LatAccelG, &c.GearAtApex,
			&banked, &c.RollDegAtApex, &c.LengthM); err != nil {
			return fmt.Errorf("rkb: corner scan: %w", err)
		}
		// apex_world is always a 2-element array in the source; either coord may be null.
		c.ApexWorld = []*float64{nullF(ax), nullF(az)}
		if apexIsLabel.Valid {
			b := apexIsLabel.Bool
			c.ApexIsLabel = &b
		}
		if banked.Valid {
			b := banked.Bool
			c.Banked = &b
		}
		if err := setPhases(phasesByTurn, &c); err != nil {
			return err
		}
		rec.Corners = append(rec.Corners, c)
	}
	if rec.Corners == nil {
		rec.Corners = []Corner{}
	}
	return rows.Err()
}

// loadPhasesByTurn reads every corner_phases row for slug in one query and groups it by turn, building
// a Phases object per turn (all four keys braking_zone/turn_in/apex/exit; a phase with no row for that
// turn stays nil/null). Fetching all rows up front - rather than one query per corner - keeps the
// corners loop in loadCorners from ever holding two open Rows at once against the pool-capped DB.
func (s *SQLiteStore) loadPhasesByTurn(ctx context.Context, slug string) (map[int]*Phases, error) {
	rows, err := s.db.QueryContext(ctx,
		`select turn,phase,world_x,world_z from corner_phases where slug=?`, slug)
	if err != nil {
		return nil, fmt.Errorf("rkb: phases %s: %w", slug, err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int]*Phases)
	for rows.Next() {
		var turn int
		var name string
		var x, z sql.NullFloat64
		if err := rows.Scan(&turn, &name, &x, &z); err != nil {
			return nil, fmt.Errorf("rkb: phase scan %s: %w", slug, err)
		}
		p := out[turn]
		if p == nil {
			p = &Phases{}
			out[turn] = p
		}
		pt := []*float64{nullF(x), nullF(z)}
		switch name {
		case "braking_zone":
			p.BrakingZone = &pt
		case "turn_in":
			p.TurnIn = &pt
		case "apex":
			p.Apex = &pt
		case "exit":
			p.Exit = &pt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rkb: phases %s: %w", slug, err)
	}
	return out, nil
}

// setPhases resolves the 3-way corners[].phases contract (fidelity rule 1), keyed on corner source:
//   - kinematic       -> a phases object with all four keys; a phase with no corner_phases row is null.
//   - derived-reverse -> literal JSON null.
//   - stored/outline_curvature (any other) -> nil, so the omitempty key is omitted entirely.
func setPhases(phasesByTurn map[int]*Phases, c *Corner) error {
	switch c.Source {
	case "kinematic":
		p := phasesByTurn[c.Turn]
		if p == nil {
			p = &Phases{}
		}
		raw, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("rkb: phases marshal t%d: %w", c.Turn, err)
		}
		c.Phases = raw
	case "derived-reverse":
		c.Phases = json.RawMessage("null")
	}
	return nil
}

func (s *SQLiteStore) loadSectors(ctx context.Context, rec *Record) error {
	rows, err := s.db.QueryContext(ctx,
		// Order by rowid (insertion order) to reproduce the source sector sequence: sector labels are
		// strings ("S1".."S16"), so a lexical `order by sector` would wrongly place S10 before S2.
		`select sector,turns,label_x,label_z from sectors where slug=? order by rowid`, rec.Slug)
	if err != nil {
		return fmt.Errorf("rkb: sectors %s: %w", rec.Slug, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sec Sector
		var turns string
		var lx, lz sql.NullFloat64
		if err := rows.Scan(&sec.Sector, &turns, &lx, &lz); err != nil {
			return fmt.Errorf("rkb: sector scan: %w", err)
		}
		sec.Turns = parseTurns(turns)
		// label_world is emitted only when both coords are present; a null coord degenerates to null
		// (the round-trip gate folds source [x,null]/[null,null] to null - fidelity rule 3).
		if lx.Valid && lz.Valid {
			v := []float64{lx.Float64, lz.Float64}
			sec.LabelWorld = &v
		}
		rec.Sectors = append(rec.Sectors, sec)
	}
	if rec.Sectors == nil {
		rec.Sectors = []Sector{}
	}
	return rows.Err()
}

// parseTurns turns "1,2,3" into []int{1,2,3}; "" -> []int{}.
func parseTurns(s string) []int {
	if s == "" {
		return []int{}
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

// loadBoundary reconstructs the boundary sides from boundary_points, dispatching each row to the side
// it belongs to (outline/left/right/cl) in idx order. Only sides with rows are populated; absent sides
// stay nil so their omitempty JSON key is omitted, matching source (SVG=outline only, victorydash=
// left/right/cl only - fidelity rule 5). No track stores an empty side, so nil-vs-empty never diverges.
func (s *SQLiteStore) loadBoundary(ctx context.Context, rec *Record) error {
	rows, err := s.db.QueryContext(ctx,
		`select side,world_x,world_z from boundary_points where slug=? order by side,idx`, rec.Slug)
	if err != nil {
		return fmt.Errorf("rkb: boundary %s: %w", rec.Slug, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var side string
		var x, z sql.NullFloat64
		if err := rows.Scan(&side, &x, &z); err != nil {
			return fmt.Errorf("rkb: boundary scan: %w", err)
		}
		pt := []*float64{nullF(x), nullF(z)}
		switch side {
		case "outline":
			rec.Boundary.Outline = append(rec.Boundary.Outline, pt)
		case "left":
			rec.Boundary.Left = append(rec.Boundary.Left, pt)
		case "right":
			rec.Boundary.Right = append(rec.Boundary.Right, pt)
		case "cl":
			rec.Boundary.Cl = append(rec.Boundary.Cl, pt)
		}
	}
	return rows.Err()
}

// ReferenceTime looks up a reference time; found=false when no row matches (empty table in v1).
func (s *SQLiteStore) ReferenceTime(ctx context.Context, slug, sector string, carID int, pp float64) (int, bool, error) {
	var ms sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`select reference_time_ms from reference_times
		 where slug=? and sector=? and car_id=? and performance_point=?`,
		slug, sector, carID, pp).Scan(&ms)
	if err == sql.ErrNoRows || (err == nil && !ms.Valid) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("rkb: reference_time %s: %w", slug, err)
	}
	return int(ms.Int64), true, nil
}
