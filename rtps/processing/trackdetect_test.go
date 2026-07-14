package processing_test

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// fakeGeomProvider is a test double for processing.DetectionGeomProvider: it returns a fixed
// geometry list (or an error) without touching rkb.Store, exercising the narrow-interface seam.
type fakeGeomProvider struct {
	geoms []rkb.TrackGeom
	err   error
}

func (f *fakeGeomProvider) DetectionGeometry(context.Context) ([]rkb.TrackGeom, error) {
	return f.geoms, f.err
}

// posFrame builds an in-race (or not) frame at world position (x, z) with the given current_lap and
// package_id, for feeding TrackDetector.OnFrame.
func posFrame(inRace bool, x, z float32, curLap int32, pkgID uint32) *telemetryv1.TelemetryFrame {
	return &telemetryv1.TelemetryFrame{
		InRace:     inRace,
		PositionX:  x,
		PositionZ:  z,
		CurrentLap: curLap,
		PackageId:  pkgID,
	}
}

func TestTrackDetector_OnSessionStart_ClearsStaleTrack(t *testing.T) {
	enrich := live.NewEnrichmentState()
	slug, conf := "high-speed-ring", "confident"
	enrich.SetTrack("s0", &slug, &conf)

	geom := &fakeGeomProvider{geoms: []rkb.TrackGeom{{Slug: "high-speed-ring", Points: [][2]float64{{0, 0}, {1, 1}}}}}
	d := processing.NewTrackDetector(geom, enrich, nil)

	d.OnSessionStart(&session.Record{ID: "s1"})

	got := enrich.Get()
	if got.Track != nil {
		t.Fatalf("expected Track cleared after OnSessionStart, got %v", *got.Track)
	}
	if got.TrackConfidence != nil {
		t.Fatalf("expected TrackConfidence cleared after OnSessionStart, got %v", *got.TrackConfidence)
	}
}

func TestTrackDetector_OnFrame_AccumulatesOnlyInRace(t *testing.T) {
	enrich := live.NewEnrichmentState()
	geom := &fakeGeomProvider{geoms: []rkb.TrackGeom{
		{Slug: "high-speed-ring", Points: [][2]float64{{0, 0}, {1, 1}}},
		{Slug: "needs-capture", Points: nil}, // must be filtered out (un-detectable)
	}}
	d := processing.NewTrackDetector(geom, enrich, nil)

	d.OnSessionStart(&session.Record{ID: "s1"})
	d.OnFrame("s1", posFrame(true, 1, 2, 0, 1))
	d.OnFrame("s1", posFrame(false, 99, 99, 0, 2)) // not in race: must be ignored
	d.OnFrame("s1", posFrame(true, 3, 4, 0, 3))

	if got := d.PointCountForTest("s1"); got != 2 {
		t.Fatalf("expected 2 accumulated points (in-race only), got %d", got)
	}
}

func TestTrackDetector_StubMatcher_LeavesTrackNil(t *testing.T) {
	enrich := live.NewEnrichmentState()
	geom := &fakeGeomProvider{geoms: []rkb.TrackGeom{
		{Slug: "high-speed-ring", Points: [][2]float64{{0, 0}, {1, 1}}},
	}}
	d := processing.NewTrackDetector(geom, enrich, nil)

	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	for i := 0; i < 10; i++ {
		d.OnFrame("s1", posFrame(true, float32(i), float32(i), 0, uint32(i)))
	}
	d.OnSessionEnd(rec)

	got := enrich.Get()
	if got.Track != nil {
		t.Fatalf("expected Track to stay nil with the stub matcher, got %v", *got.Track)
	}
}

// fakeEnricher is a test double for processing.TrackEnricher: it records every SetTrack call (not just
// the final state), so tests can assert call COUNTS - e.g. that latching stops a second SetTrack from
// ever happening, not just that the last-observed value looks right.
type fakeEnricher struct {
	mu    sync.Mutex
	calls []setTrackCall
}

// setTrackCall is one recorded SetTrack invocation, with the pointer args dereferenced for easy
// comparison in test assertions.
type setTrackCall struct {
	sessionID  string
	slug       string
	confidence string
}

func (f *fakeEnricher) SetTrack(sessionID string, slug *string, confidence *string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, setTrackCall{sessionID: sessionID, slug: *slug, confidence: *confidence})
}

func (f *fakeEnricher) ClearTrack(string) {}

// callCount returns the number of recorded SetTrack calls so far.
func (f *fakeEnricher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// lastCall returns the most recent recorded SetTrack call. Callers must check callCount() > 0 first.
func (f *fakeEnricher) lastCall() setTrackCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// circlePoints returns n points evenly spaced around a circle of the given radius centered at (cx, cz):
// a simple, fully deterministic "distinct loop" shape for building fake reference geometry, with no
// dependency on any real track data.
func circlePoints(cx, cz, radius float64, n int) [][2]float64 {
	pts := make([][2]float64, n)
	for i := 0; i < n; i++ {
		theta := 2 * math.Pi * float64(i) / float64(n)
		pts[i] = [2]float64{cx + radius*math.Cos(theta), cz + radius*math.Sin(theta)}
	}
	return pts
}

// jitterPoints returns a copy of points with a small, fixed (non-random) alternating offset applied to
// each - simulating driven-path measurement noise without any nondeterministic source, per the task's
// "no Math.random" constraint.
func jitterPoints(points [][2]float64) [][2]float64 {
	out := make([][2]float64, len(points))
	for i, p := range points {
		dx, dz := 0.5, -0.5
		if i%2 == 0 {
			dx, dz = -0.5, 0.5
		}
		out[i] = [2]float64{p[0] + dx, p[1] + dz}
	}
	return out
}

// feedFrames drives one in-race OnFrame call per point in points, for sessionID.
func feedFrames(d *processing.TrackDetector, sessionID string, points [][2]float64) {
	for i, p := range points {
		d.OnFrame(sessionID, posFrame(true, float32(p[0]), float32(p[1]), 0, uint32(i)))
	}
}

// TestTrackDetector_AttemptMatch_ResolvesToDistinctTrack drives a jittered sample of track A's own
// points against a candidate set of [A, B] where B is a clearly different shape far away: the shape
// score should land close to A (small mean NN distance) and far from B (huge distance), giving a clear
// margin, so the match should resolve confidently to A.
func TestTrackDetector_AttemptMatch_ResolvesToDistinctTrack(t *testing.T) {
	trackA := circlePoints(0, 0, 500, 60)
	trackB := circlePoints(100000, 100000, 2000, 20)

	enrich := &fakeEnricher{}
	geom := &fakeGeomProvider{geoms: []rkb.TrackGeom{
		{Slug: "track-a", Points: trackA},
		{Slug: "track-b", Points: trackB},
	}}
	d := processing.NewTrackDetector(geom, enrich, nil)

	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", jitterPoints(trackA[:45]))
	d.OnSessionEnd(rec)

	if enrich.callCount() == 0 {
		t.Fatalf("expected a confident match to be published, got no SetTrack calls")
	}
	got := enrich.lastCall()
	if got.slug != "track-a" {
		t.Fatalf("expected match to resolve to track-a, got %q", got.slug)
	}
	if got.confidence != "confident" {
		t.Fatalf("expected confidence %q, got %q", "confident", got.confidence)
	}
}

// TestTrackDetector_AttemptMatch_AmbiguousStaysNil feeds the same jittered sample of track A against a
// candidate set of [A, C] where C is a near-duplicate (identical point cloud, e.g. a reversed layout
// that has not been direction-differentiated yet): both candidates score almost identically, so the
// margin requirement should not be met and no match should be published.
func TestTrackDetector_AttemptMatch_AmbiguousStaysNil(t *testing.T) {
	trackA := circlePoints(0, 0, 500, 60)
	trackC := append([][2]float64(nil), trackA...) // near-duplicate: identical shape

	enrich := &fakeEnricher{}
	geom := &fakeGeomProvider{geoms: []rkb.TrackGeom{
		{Slug: "track-a", Points: trackA},
		{Slug: "track-c", Points: trackC},
	}}
	d := processing.NewTrackDetector(geom, enrich, nil)

	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", jitterPoints(trackA[:45]))
	d.OnSessionEnd(rec)

	if got := enrich.callCount(); got != 0 {
		t.Fatalf("expected no confident match between near-duplicate tracks, got %d calls (%+v)", got, enrich.calls)
	}
}

// TestTrackDetector_AttemptMatch_PartialCoverageStaysNil feeds fewer points than minCoveragePoints: no
// match attempt should even be made, so no SetTrack call should ever happen.
func TestTrackDetector_AttemptMatch_PartialCoverageStaysNil(t *testing.T) {
	trackA := circlePoints(0, 0, 500, 60)

	enrich := &fakeEnricher{}
	geom := &fakeGeomProvider{geoms: []rkb.TrackGeom{{Slug: "track-a", Points: trackA}}}
	d := processing.NewTrackDetector(geom, enrich, nil)

	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", jitterPoints(trackA[:10])) // well below minCoveragePoints
	d.OnSessionEnd(rec)

	if got := enrich.callCount(); got != 0 {
		t.Fatalf("expected no match below coverage threshold, got %d calls (%+v)", got, enrich.calls)
	}
}

// TestTrackDetector_AttemptMatch_LatchesAfterConfidentMatch confirms that once a confident match is
// published, further frames (even more of the exact same track's points) never trigger a second
// SetTrack call - the session is latched and left alone.
func TestTrackDetector_AttemptMatch_LatchesAfterConfidentMatch(t *testing.T) {
	trackA := circlePoints(0, 0, 500, 60)
	trackB := circlePoints(100000, 100000, 2000, 20)

	enrich := &fakeEnricher{}
	geom := &fakeGeomProvider{geoms: []rkb.TrackGeom{
		{Slug: "track-a", Points: trackA},
		{Slug: "track-b", Points: trackB},
	}}
	d := processing.NewTrackDetector(geom, enrich, nil)

	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", jitterPoints(trackA[:45]))

	if got := enrich.callCount(); got != 1 {
		t.Fatalf("expected exactly one SetTrack call once confident, got %d (%+v)", got, enrich.calls)
	}

	// Feed more frames after latching: must not re-match or overwrite.
	feedFrames(d, "s1", jitterPoints(trackA[:20]))
	d.OnSessionEnd(rec)

	if got := enrich.callCount(); got != 1 {
		t.Fatalf("expected latch to prevent further SetTrack calls, got %d (%+v)", got, enrich.calls)
	}
	if got := enrich.lastCall(); got.slug != "track-a" {
		t.Fatalf("expected latched match to stay track-a, got %q", got.slug)
	}
}

func TestTrackDetector_OnSessionStart_GeomLoadErrorLeavesGeometryEmpty(t *testing.T) {
	enrich := live.NewEnrichmentState()
	geom := &fakeGeomProvider{err: errors.New("boom")}
	d := processing.NewTrackDetector(geom, enrich, nil)

	// Must not panic, and OnFrame must simply no-op afterward.
	d.OnSessionStart(&session.Record{ID: "s1"})
	d.OnFrame("s1", posFrame(true, 1, 2, 0, 1))

	if got := d.PointCountForTest("s1"); got != 1 {
		t.Fatalf("expected accumulator still works even when geometry failed to load, got %d", got)
	}
}

// reversePoints returns a copy of points in reverse order: used to simulate driving a track's known
// geometry in the opposite direction of travel, without any dependency on randomness.
func reversePoints(points [][2]float64) [][2]float64 {
	out := make([][2]float64, len(points))
	for i, p := range points {
		out[len(points)-1-i] = p
	}
	return out
}

// twinGeoms returns a forward/reverse layout pair sharing Base=1, modeling the REAL RKB shape: each
// candidate's Points is boundary ++ apexes (see rtps/rkb TrackGeom/DetectionGeometry), where the reverse
// layout's boundary prefix is a VERBATIM copy of the forward's, but its apex suffix is in REVERSED order
// (tools/rkb/rkb/reverse.py's _derive_corners reverses the forward's corner list). A flat, order-preserved
// copy of the whole cloud (boundary+apex together) does NOT model this - it would pass even a naive
// per-index comparison and so could never catch the per-index-vs-order-insensitive bug this fixture
// exists to exercise.
func twinGeoms(boundary, apexes [][2]float64) []rkb.TrackGeom {
	forwardPoints := append(append([][2]float64(nil), boundary...), apexes...)
	reversedApexes := reversePoints(apexes)
	reverseLayoutPoints := append(append([][2]float64(nil), boundary...), reversedApexes...)
	return []rkb.TrackGeom{
		{Slug: "track-a", Base: 1, IsReverse: false, Points: forwardPoints},
		{Slug: "track-a-reverse", Base: 1, IsReverse: true, Points: reverseLayoutPoints},
	}
}

// TestTrackDetector_AttemptMatch_IdenticalTwinResolvesToBase_ForwardDrivenPath drives track A's own
// geometry, traced forward, against a [fwd, reverse] twin pair sharing an identical point cloud: the
// shape score ties the twins exactly, but since they are the SAME physical track (identical geometry,
// opposite IsReverse) the tie must be resolved rather than rejected as ambiguous, landing on the base
// (IsReverse=false) slug. Direction of the driven path is irrelevant to this feature now - only the
// physical track identity matters - so a forward-traced path resolving to the base slug is one of two
// traversal directions this test (paired with the reverse-driven variant below) exercises.
func TestTrackDetector_AttemptMatch_IdenticalTwinResolvesToBase_ForwardDrivenPath(t *testing.T) {
	trackA := circlePoints(0, 0, 500, 60)
	boundary, apexes := trackA[:50], trackA[50:] // model Points = boundary ++ apexes (see rtps/rkb TrackGeom)

	enrich := &fakeEnricher{}
	geom := &fakeGeomProvider{geoms: twinGeoms(boundary, apexes)}
	d := processing.NewTrackDetector(geom, enrich, nil)

	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", jitterPoints(trackA[:45]))
	d.OnSessionEnd(rec)

	if got := enrich.callCount(); got != 1 {
		t.Fatalf("expected exactly one SetTrack call, got %d (%+v)", got, enrich.calls)
	}
	got := enrich.lastCall()
	if got.slug != "track-a" {
		t.Fatalf("expected identical-geometry twin to resolve to base slug track-a, got %q", got.slug)
	}
	if got.confidence != "confident" {
		t.Fatalf("expected confidence %q, got %q", "confident", got.confidence)
	}
}

// TestTrackDetector_AttemptMatch_IdenticalTwinResolvesToBase_ReverseDrivenPath drives the SAME geometry
// as the forward-driven test above, but traversed in the opposite order (simulating driving the reverse
// layout). Since direction is no longer this feature's job, this must STILL resolve to the base slug
// track-a (never track-a-reverse) with full confidence - only the physical track identity is reported.
func TestTrackDetector_AttemptMatch_IdenticalTwinResolvesToBase_ReverseDrivenPath(t *testing.T) {
	trackA := circlePoints(0, 0, 500, 60)
	boundary, apexes := trackA[:50], trackA[50:] // model Points = boundary ++ apexes (see rtps/rkb TrackGeom)

	enrich := &fakeEnricher{}
	geom := &fakeGeomProvider{geoms: twinGeoms(boundary, apexes)}
	d := processing.NewTrackDetector(geom, enrich, nil)

	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", reversePoints(jitterPoints(trackA[:45])))
	d.OnSessionEnd(rec)

	if got := enrich.callCount(); got != 1 {
		t.Fatalf("expected exactly one SetTrack call, got %d (%+v)", got, enrich.calls)
	}
	got := enrich.lastCall()
	if got.slug != "track-a" {
		t.Fatalf("expected reverse-driven path to still resolve to base slug track-a (direction is irrelevant), got %q", got.slug)
	}
	if got.confidence != "confident" {
		t.Fatalf("expected confidence %q, got %q", "confident", got.confidence)
	}
}

// nudgePoints returns a copy of points with a fixed (non-random) uniform offset applied to every point:
// used to build a candidate whose geometry is DIFFERENT from another (by more than twinGeometryEpsilon)
// while still being close enough in shape to tie on nearest-neighbour score.
func nudgePoints(points [][2]float64, delta float64) [][2]float64 {
	out := make([][2]float64, len(points))
	for i, p := range points {
		out[i] = [2]float64{p[0] + delta, p[1] + delta}
	}
	return out
}

// TestTrackDetector_AttemptMatch_NonIdenticalTieStaysAmbiguous drives a jittered sample of track A
// against a candidate pair that share a tracks.base value and have opposite IsReverse (so a Base-only
// twin heuristic would wrongly resolve them) but whose point clouds are NOT identical (candidate D is
// track A's points nudged by a few centimetres - well beyond twinGeometryEpsilon): a genuinely different
// layout that merely ties on shape score must still be rejected as ambiguous, not resolved as a twin.
func TestTrackDetector_AttemptMatch_NonIdenticalTieStaysAmbiguous(t *testing.T) {
	trackA := circlePoints(0, 0, 500, 60)
	trackD := nudgePoints(trackA, 0.02)

	enrich := &fakeEnricher{}
	geom := &fakeGeomProvider{geoms: []rkb.TrackGeom{
		{Slug: "track-a", Base: 1, IsReverse: false, Points: trackA},
		{Slug: "track-d", Base: 1, IsReverse: true, Points: trackD},
	}}
	d := processing.NewTrackDetector(geom, enrich, nil)

	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", jitterPoints(trackA[:45]))
	d.OnSessionEnd(rec)

	if got := enrich.callCount(); got != 0 {
		t.Fatalf("expected non-identical-geometry tie to stay ambiguous, got %d calls (%+v)", got, enrich.calls)
	}
}

// TestBboxContains validates the bbox containment pre-filter used in matchDecision: driven bbox must
// be fully contained within candidate bbox (widened by tolerance) for a potential match.
func TestBboxContains(t *testing.T) {
	const tol = processing.BboxToleranceForTest // Use the const value via test export
	tests := []struct {
		name         string
		driven, cand [4]float64
		tol          float64
		want         bool
	}{
		{
			name:   "driven fully inside candidate",
			driven: [4]float64{10, 10, 20, 20},
			cand:   [4]float64{0, 0, 30, 30},
			tol:    tol,
			want:   true,
		},
		{
			name:   "driven poking within tolerance of edge",
			driven: [4]float64{-1, -1, 31, 31},
			cand:   [4]float64{0, 0, 30, 30},
			tol:    tol,
			want:   true,
		},
		{
			name:   "driven clearly outside (far beyond)",
			driven: [4]float64{100, 100, 200, 200},
			cand:   [4]float64{0, 0, 30, 30},
			tol:    tol,
			want:   false,
		},
		{
			name:   "driven poking just beyond tolerance",
			driven: [4]float64{-(tol + 1), -(tol + 1), 30 + tol + 1, 30 + tol + 1},
			cand:   [4]float64{0, 0, 30, 30},
			tol:    tol,
			want:   false,
		},
		{
			name:   "driven touches candidate boundary exactly",
			driven: [4]float64{0, 0, 30, 30},
			cand:   [4]float64{0, 0, 30, 30},
			tol:    tol,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := processing.BboxContainsForTest(tt.driven, tt.cand, tt.tol)
			if got != tt.want {
				t.Errorf("bboxContains(%v, %v, %f) = %v, want %v", tt.driven, tt.cand, tt.tol, got, tt.want)
			}
		})
	}
}

// realTwinPair is one real forward/reverse layout pair from the generated RKB corpus: both slugs share
// tracks.base and have opposite is_reverse, and (per tools/rkb/rkb/reverse.py) the reverse layout's
// boundary is a verbatim copy of the forward's with its apex/corner order reversed - exactly the shape
// the identicalGeometry order-insensitive fix exists to recognise. Sourced by inspecting
// tools/rkb/out/rkb.db: these are the pairs whose reverse member carries provenance_corners =
// "derived-reverse" AND has is_reverse=1 (i.e. an actual opposite-IsReverse twin, unlike the
// tokyo-express-east-cw/ccw pair, which also carries derived-reverse provenance but is NOT an
// opposite-IsReverse pair - both sides have is_reverse=0 - so it is out of scope for this feature and
// intentionally excluded here).
type realTwinPair struct{ forward, reverse string }

var realTwinPairs = []realTwinPair{
	{"alsace-test", "alsace-test-2"},
	{"grand-valley-south", "grand-valley-south-2"},
	{"fishermans-ranch", "fishermans-ranch-2"},
	{"lago-maggiore-east-end", "lago-maggiore-east-end-2"},
	{"willow-springs-horse-thief-mile", "willow-springs-horse-thief-mile-2"},
}

// TestTrackDetector_ForwardReverseTwin_RealCorpus is the regression guard for the per-index-vs-
// order-insensitive geometry bug: skipped cleanly when tools/rkb/out/rkb.db is absent (same pattern as
// rtps/rkb's TestSQLiteStore_DetectionGeometry_RealDB_NoEmptyPoints). Otherwise it loads the real
// DetectionGeometry and asserts that forwardReverseTwin (via candidateScore) actually recognises each
// real forward/reverse pair in realTwinPairs as a twin - proving the fix resolves real data, not just the
// synthetic fixtures above. It also asserts that two DIFFERENT layouts sharing the same nonzero Base
// (lago-maggiore-centre vs. lago-maggiore-east-end-2: same base, opposite IsReverse, but not the same
// physical layout) are correctly NOT recognised as twins.
func TestTrackDetector_ForwardReverseTwin_RealCorpus(t *testing.T) {
	dbPath := filepath.Join("..", "..", "tools", "rkb", "out", "rkb.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skip("generated rkb.db not present; run `cd tools/rkb && ./.venv/bin/python -m rkb`")
	}

	st, err := rkb.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	geoms, err := st.DetectionGeometry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bySlug := make(map[string]rkb.TrackGeom, len(geoms))
	for _, g := range geoms {
		bySlug[g.Slug] = g
	}

	for _, pair := range realTwinPairs {
		t.Run(pair.forward+"/"+pair.reverse, func(t *testing.T) {
			fwd, ok := bySlug[pair.forward]
			if !ok {
				t.Fatalf("forward slug %q not found in real corpus", pair.forward)
			}
			rev, ok := bySlug[pair.reverse]
			if !ok {
				t.Fatalf("reverse slug %q not found in real corpus", pair.reverse)
			}

			slug, ok := processing.ForwardReverseTwinForTest(
				fwd.Slug, fwd.IsReverse, fwd.Base, fwd.Points,
				rev.Slug, rev.IsReverse, rev.Base, rev.Points,
			)
			if !ok {
				t.Fatalf("expected %s/%s to be recognised as a real forward/reverse twin, got ambiguous", pair.forward, pair.reverse)
			}
			if slug != pair.forward {
				t.Fatalf("expected twin to resolve to base slug %q, got %q", pair.forward, slug)
			}
		})
	}

	// Different same-Base layouts must stay ambiguous: same base=3 as the lago-maggiore pair above and
	// opposite IsReverse, but a genuinely different physical layout.
	centre, ok := bySlug["lago-maggiore-centre"]
	if !ok {
		t.Fatal("lago-maggiore-centre not found in real corpus")
	}
	eastEnd2, ok := bySlug["lago-maggiore-east-end-2"]
	if !ok {
		t.Fatal("lago-maggiore-east-end-2 not found in real corpus")
	}
	if _, ok := processing.ForwardReverseTwinForTest(
		centre.Slug, centre.IsReverse, centre.Base, centre.Points,
		eastEnd2.Slug, eastEnd2.IsReverse, eastEnd2.Base, eastEnd2.Points,
	); ok {
		t.Fatal("expected different same-Base layouts (lago-maggiore-centre vs lago-maggiore-east-end-2) to stay ambiguous, got recognised as a twin")
	}
}
