package rkb

import (
	"context"
	"database/sql"
	"fmt"
)

// detectionPointStride is the subsampling stride applied to each track's boundary-point source (keep
// every Nth point) before it is handed to the live track detector's shape matcher. Tunable: smaller
// keeps more geometric detail at the cost of a larger point cloud per track; larger trades fidelity
// for a cheaper per-frame match. Apex points are never subsampled (there are few per track).
const detectionPointStride = 8

// TrackGeom is a lightweight per-track reference-geometry record for live track detection: enough to
// bbox-prefilter and shape-match a live position stream against, without assembling a full Record.
type TrackGeom struct {
	Slug      string
	IsReverse bool
	// Base identifies a forward/reverse layout pair: a forward track and its reverse twin share the
	// same Base value (from the tracks.base column, itself sourced from the in-game course DB's "Base"
	// id) while differing in IsReverse. 0 means the column was NULL (no pairing information; a track
	// with Base==0 is never treated as another Base==0 track's twin).
	Base int
	// Bbox is [world_bbox_min_x, world_bbox_min_z, world_bbox_max_x, world_bbox_max_z]; meaningful only
	// when HasBbox is true.
	Bbox    [4]float64
	HasBbox bool // false when any of the 4 world_bbox columns is NULL (~23 tracks) - detector must skip the bbox pre-filter for these.
	// Points is the track's world-metre point cloud used for shape matching: the preferred boundary
	// source (cl > left+right > outline), subsampled by detectionPointStride, followed by all corner
	// apex_world points (unsampled).
	Points [][2]float64
}

// boundarySide is one row of the boundary_points table, scoped to the columns DetectionGeometry needs.
type boundarySide struct {
	side string
	x, z sql.NullFloat64
}

// DetectionGeometry returns the lightweight reference geometry for every track, built from exactly
// three queries (tracks, boundary_points, corners) regardless of track count - never one query per
// track - so it stays cheap enough to load whole into the live detector at startup.
func (s *SQLiteStore) DetectionGeometry(ctx context.Context) ([]TrackGeom, error) {
	geoms, order, err := s.detectionTracks(ctx)
	if err != nil {
		return nil, err
	}
	boundaryBySlug, err := s.detectionBoundary(ctx)
	if err != nil {
		return nil, err
	}
	apexBySlug, err := s.detectionApexes(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]TrackGeom, 0, len(order))
	for _, slug := range order {
		g := geoms[slug]
		g.Points = append(subsampleBoundary(boundaryBySlug[slug]), apexBySlug[slug]...)
		out = append(out, g)
	}
	return out, nil
}

// detectionTracks reads slug/is_reverse/world_bbox for every track, in slug order (order is returned
// separately so the final assembly loop is deterministic).
func (s *SQLiteStore) detectionTracks(ctx context.Context) (map[string]TrackGeom, []string, error) {
	rows, err := s.db.QueryContext(ctx,
		`select slug,base,is_reverse,
		        affine_world_bbox_min_x,affine_world_bbox_min_z,affine_world_bbox_max_x,affine_world_bbox_max_z
		 from tracks order by slug`)
	if err != nil {
		return nil, nil, fmt.Errorf("rkb: detection tracks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	geoms := make(map[string]TrackGeom)
	var order []string
	for rows.Next() {
		var slug string
		var base sql.NullInt64
		var isReverse sql.NullInt64
		var minX, minZ, maxX, maxZ sql.NullFloat64
		if err := rows.Scan(&slug, &base, &isReverse, &minX, &minZ, &maxX, &maxZ); err != nil {
			return nil, nil, fmt.Errorf("rkb: detection tracks scan: %w", err)
		}
		g := TrackGeom{Slug: slug, IsReverse: isReverse.Valid && isReverse.Int64 == 1}
		if base.Valid {
			g.Base = int(base.Int64)
		}
		if minX.Valid && minZ.Valid && maxX.Valid && maxZ.Valid {
			g.HasBbox = true
			g.Bbox = [4]float64{minX.Float64, minZ.Float64, maxX.Float64, maxZ.Float64}
		}
		geoms[slug] = g
		order = append(order, slug)
	}
	return geoms, order, rows.Err()
}

// detectionBoundary reads every boundary_points row once and groups it by slug, ordered by side then
// idx (matching insertion order per side) so per-slug source selection and subsampling see a stable
// sequence.
func (s *SQLiteStore) detectionBoundary(ctx context.Context) (map[string][]boundarySide, error) {
	rows, err := s.db.QueryContext(ctx,
		`select slug,side,world_x,world_z from boundary_points order by slug,side,idx`)
	if err != nil {
		return nil, fmt.Errorf("rkb: detection boundary: %w", err)
	}
	defer func() { _ = rows.Close() }()

	bySlug := make(map[string][]boundarySide)
	for rows.Next() {
		var slug string
		var bp boundarySide
		if err := rows.Scan(&slug, &bp.side, &bp.x, &bp.z); err != nil {
			return nil, fmt.Errorf("rkb: detection boundary scan: %w", err)
		}
		bySlug[slug] = append(bySlug[slug], bp)
	}
	return bySlug, rows.Err()
}

// detectionApexes reads every corner's apex_world once and groups it by slug, keeping only apexes
// where both coordinates are non-null (a bare corner label with no resolved apex contributes nothing
// to the shape).
func (s *SQLiteStore) detectionApexes(ctx context.Context) (map[string][][2]float64, error) {
	rows, err := s.db.QueryContext(ctx,
		`select slug,apex_x,apex_z from corners order by slug,turn`)
	if err != nil {
		return nil, fmt.Errorf("rkb: detection corners: %w", err)
	}
	defer func() { _ = rows.Close() }()

	bySlug := make(map[string][][2]float64)
	for rows.Next() {
		var slug string
		var x, z sql.NullFloat64
		if err := rows.Scan(&slug, &x, &z); err != nil {
			return nil, fmt.Errorf("rkb: detection corners scan: %w", err)
		}
		if x.Valid && z.Valid {
			bySlug[slug] = append(bySlug[slug], [2]float64{x.Float64, z.Float64})
		}
	}
	return bySlug, rows.Err()
}

// subsampleBoundary picks the preferred boundary-point source for one track (cl > left+right >
// outline), preferring the highest-priority source in that order that actually yields at least one
// valid point once nulls are filtered and the stride is applied. A source having rows is not enough:
// if its rows are all null-coord at the kept stride indices it would otherwise contribute nothing, so
// selection falls through to the next-priority source rather than returning an empty point cloud.
func subsampleBoundary(pts []boundarySide) [][2]float64 {
	var cl, left, right, outline []boundarySide
	for _, p := range pts {
		switch p.side {
		case "cl":
			cl = append(cl, p)
		case "left":
			left = append(left, p)
		case "right":
			right = append(right, p)
		case "outline":
			outline = append(outline, p)
		}
	}

	if out := filterAndStride(cl); len(out) > 0 {
		return out
	}
	if out := filterAndStride(append(append([]boundarySide{}, left...), right...)); len(out) > 0 {
		return out
	}
	return filterAndStride(outline)
}

// filterAndStride drops points with any null coordinate, then keeps every detectionPointStride'th
// point of what remains (a shape-matching point cloud has no use for a half-known vertex, and
// filtering before striding ensures the stride is applied over the actually-usable points rather than
// landing on indices that happen to be null).
func filterAndStride(pts []boundarySide) [][2]float64 {
	valid := make([]boundarySide, 0, len(pts))
	for _, p := range pts {
		if p.x.Valid && p.z.Valid {
			valid = append(valid, p)
		}
	}

	out := make([][2]float64, 0, len(valid)/detectionPointStride+1)
	for i, p := range valid {
		if i%detectionPointStride != 0 {
			continue
		}
		out = append(out, [2]float64{p.x.Float64, p.z.Float64})
	}
	return out
}
