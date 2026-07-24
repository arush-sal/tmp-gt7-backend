// Package rkbref adapts the RKB store (rkb.Store) to the backend's reference.KB contract and,
// structurally, to processing.SectorProvider (the serving path). It ports the frontend's
// arc-length sector-partition algorithm (frontend/src/lib/sectorTiming.ts) into Go, improving the
// label->centerline projection from nearest-vertex to segment interpolation.
//
// It deliberately does NOT import the processing package: SectorsForSession merely matches that
// method's signature so a *KB satisfies processing.SectorProvider by Go structural typing, keeping
// the dependency edge one-way (processing -> reference, api -> rkbref).
package rkbref

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/arush-sal/garage/backend/rtps/geom"
	"github.com/arush-sal/garage/backend/rtps/reference"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"go.uber.org/zap"
)

// KB wires the RKB store to reference.KB. slugFor resolves a session id to its detected track slug
// (from live enrichment); the store then yields that track's canonical record.
type KB struct {
	store   rkb.Store
	slugFor func(sessionID string) (string, bool)
	log     *zap.Logger

	mu     sync.Mutex      // guards warned
	warned map[string]bool // slug -> already emitted the "geometry missing" warning
}

// New wires the adapter. slugFor maps a session id to its detected track slug (ok=false when no
// track is resolved). log may be nil (a no-op logger is used).
func New(store rkb.Store, slugFor func(sessionID string) (string, bool), log *zap.Logger) *KB {
	if log == nil {
		log = zap.NewNop()
	}
	return &KB{store: store, slugFor: slugFor, log: log, warned: map[string]bool{}}
}

// TrackForSession implements reference.KB: resolve the session's slug, then load its record.
func (k *KB) TrackForSession(ctx context.Context, sessionID string) (reference.Track, bool, error) {
	slug, ok := k.slugFor(sessionID)
	if !ok {
		return reference.Track{}, false, nil
	}
	rec, found, err := k.store.Track(ctx, slug)
	if err != nil {
		return reference.Track{}, false, err
	}
	if !found {
		return reference.Track{}, false, nil
	}
	return reference.Track{ID: slug, Name: rec.Name, LengthM: deref(rec.LengthM)}, true, nil
}

// Sectors implements reference.KB. trackID is the slug. Returns nil (not an error) when the record
// has no usable RKB geometry, so processing.SectorsForLap falls back to mini-sectors as it already
// does.
func (k *KB) Sectors(ctx context.Context, trackID string) ([]reference.Sector, error) {
	rec, found, err := k.store.Track(ctx, trackID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	secs, _ := SectorsFromRecord(rec)
	return secs, nil
}

// SectorsForSession is the serving path: same result as Sectors but with a machine-readable reason
// so the API can explain why sector timing is unavailable. A detected slug whose geometry is
// missing is logged once (warn) per slug — surfacing missing RKB data rather than hiding it.
func (k *KB) SectorsForSession(ctx context.Context, sessionID string) (sectors []reference.Sector, reason string, err error) {
	slug, ok := k.slugFor(sessionID)
	if !ok {
		return nil, "track-not-detected", nil
	}
	rec, found, err := k.store.Track(ctx, slug)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, "track-not-detected", nil
	}
	sectors, reason = SectorsFromRecord(rec)
	if reason != "" {
		k.warnOnce(slug, reason)
	}
	return sectors, reason, nil
}

// warnOnce emits the geometry-missing warning at most once per slug.
func (k *KB) warnOnce(slug, reason string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.warned[slug] {
		return
	}
	k.warned[slug] = true
	k.log.Warn("RKB geometry missing for sector timing",
		zap.String("slug", slug), zap.String("reason", reason))
}

// SectorsFromRecord derives contiguous sector boundaries from a track record's centerline + sector
// label anchors. Pure, no I/O. reason == "" with non-nil sectors means usable RKB geometry;
// otherwise sectors is nil and reason is "no-centerline" or "no-sector-labels".
//
// It ports frontend sectorTiming.ts but projects each label onto the centerline by foot-of-
// perpendicular (segment interpolation) instead of nearest-vertex, for sub-vertex arc precision.
func SectorsFromRecord(rec *rkb.Record) (sectors []reference.Sector, reason string) {
	// 1. Centerline polyline: keep only vertices with both coords present.
	cl := centerline(rec)
	if len(cl) < 2 {
		return nil, "no-centerline"
	}

	// 2. Cumulative arc length per vertex.
	arc := geom.ClArcLengths(cl)
	totalArc := arc[len(arc)-1]
	if totalArc <= 0 { // degenerate (all-coincident) centerline: no usable geometry
		return nil, "no-centerline"
	}

	// 3-4. Project each finite label onto the centerline arc axis.
	projected := make([]float64, 0, len(rec.Sectors))
	for _, s := range rec.Sectors {
		if s.LabelWorld == nil {
			continue
		}
		lw := *s.LabelWorld
		if len(lw) < 2 || !isFinite(lw[0]) || !isFinite(lw[1]) {
			continue
		}
		if a, ok := geom.ProjectToClArc(cl, arc, lw[0], lw[1]); ok {
			projected = append(projected, a)
		}
	}
	if len(projected) < 2 {
		return nil, "no-sector-labels"
	}

	// 5. Interior boundaries = arc midpoints between consecutive (sorted) labels, as fractions.
	sort.Float64s(projected)
	n := len(projected)
	sectors = make([]reference.Sector, n)
	prev := 0.0
	for i := 0; i < n; i++ {
		end := 1.0
		if i < n-1 {
			end = ((projected[i] + projected[i+1]) / 2) / totalArc
		}
		sectors[i] = reference.Sector{Index: i, Start: prev, End: end}
		prev = end
	}
	return sectors, ""
}

// centerline extracts a track record's centerline as finite (x,z) world points, dropping vertices
// with a missing coord. Shared by SectorsFromRecord and CenterlineForSession.
func centerline(rec *rkb.Record) [][2]float64 {
	cl := make([][2]float64, 0, len(rec.Boundary.Cl))
	for _, p := range rec.Boundary.Cl {
		if len(p) < 2 || p[0] == nil || p[1] == nil {
			continue
		}
		cl = append(cl, [2]float64{*p[0], *p[1]})
	}
	return cl
}

// deref returns *p, or 0 when p is nil.
func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// isFinite reports whether v is neither NaN nor infinite.
func isFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
