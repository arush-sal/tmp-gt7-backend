package processing_test

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	d.OnFrame("s1", posFrame(false, 99, 99, -1, 2)) // menu (lap -1, not in race): must be ignored
	d.OnFrame("s1", posFrame(true, 3, 4, 0, 3))
	d.OnFrame("s1", posFrame(false, 5, 6, 1, 4)) // in_race clear but lap >= 0: on track (newer GT7)

	if got := d.PointCountForTest("s1"); got != 3 {
		t.Fatalf("expected 3 accumulated points (on-track only), got %d", got)
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
// the final state) and counts ClearTrack calls, so tests can assert the whole provisional -> locked
// sequence - e.g. that a lock stops any further SetTrack, not just that the last value looks right.
type fakeEnricher struct {
	mu     sync.Mutex
	calls  []setTrackCall
	clears int
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

func (f *fakeEnricher) ClearTrack(string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clears++
}

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

// sequence returns the recorded calls as "slug/confidence" strings, for readable sequence assertions.
func (f *fakeEnricher) sequence() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.slug + "/" + c.confidence
	}
	return out
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

// Synthetic loop used by the matcher tests: a 500 m-radius circle sampled every ~5.2 m (600 points), so
// consecutive frames are a realistic step apart and travelled distance accumulates like real driving.
const (
	loopRadius = 500.0
	loopN      = 600
)

var loopLength = 2 * math.Pi * loopRadius // ~3142 m

// lapFraction returns the first frac of a jittered drive around the synthetic loop.
func lapFraction(frac float64) [][2]float64 {
	return jitterPoints(circlePoints(0, 0, loopRadius, loopN)[:int(frac*loopN)])
}

// runSession drives points through a fresh detector over geoms and returns the recorder.
func runSession(t *testing.T, geoms []rkb.TrackGeom, points [][2]float64) *fakeEnricher {
	t.Helper()
	enrich := &fakeEnricher{}
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, enrich, nil)
	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", points)
	d.OnSessionEnd(rec)
	return enrich
}

// farTrack is a distinct candidate far away from the synthetic loop (pruned by the bbox pre-filter).
func farTrack() rkb.TrackGeom {
	return rkb.TrackGeom{Slug: "track-b", Points: circlePoints(100000, 100000, 2000, 20), LengthM: 12566}
}

// TestTrackDetector_ProvisionalThenLockedOnCoverage: with a same-venue sibling layout still feasible
// (so the early margin lock cannot fire), the guess is published as provisional while the lap is being
// driven, and only locks to confident once lockCoverageFraction (0.9) of the lap length is travelled.
func TestTrackDetector_ProvisionalThenLockedOnCoverage(t *testing.T) {
	loop := circlePoints(0, 0, loopRadius, loopN)
	sibling := circlePoints(0, 0, 600, 120) // same venue, bigger loop: bbox-feasible all lap, fits ~100 m off
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Base: 7, Points: loop, LengthM: loopLength},
		{Slug: "track-a-long", Base: 7, Points: sibling, LengthM: 2 * math.Pi * 600},
		farTrack(),
	}

	half := runSession(t, geoms, lapFraction(0.5))
	if got := half.sequence(); len(got) != 1 || got[0] != "track-a/provisional" {
		t.Fatalf("half a lap: want [track-a/provisional], got %v", got)
	}

	full := runSession(t, geoms, lapFraction(0.95))
	if got := full.sequence(); len(got) != 2 || got[0] != "track-a/provisional" || got[1] != "track-a/confident" {
		t.Fatalf("0.95 lap: want [track-a/provisional track-a/confident], got %v", got)
	}
}

// TestTrackDetector_MarginLocksBeforeFullLap: with no sibling layout and the runner-up far behind, the
// match locks early - after lockMarginMinFraction of the lap, well before full coverage.
func TestTrackDetector_MarginLocksBeforeFullLap(t *testing.T) {
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength},
		farTrack(),
	}
	enrich := runSession(t, geoms, lapFraction(0.45))
	if enrich.callCount() == 0 || enrich.lastCall() != (setTrackCall{"s1", "track-a", "confident"}) {
		t.Fatalf("want an early confident lock on track-a, got %v", enrich.sequence())
	}
}

// TestTrackDetector_LockSurvivesShortExcursion: once confident, a short trip off the layout (a pit
// lane, a run-off, a wide moment: here ~190 m driven beyond releaseDistM) neither publishes nor clears.
func TestTrackDetector_LockSurvivesShortExcursion(t *testing.T) {
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength},
		farTrack(),
	}
	enrich := &fakeEnricher{}
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, enrich, nil)
	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", lapFraction(0.95))
	calls, clears := enrich.callCount(), enrich.clears
	if enrich.lastCall().confidence != "confident" {
		t.Fatalf("want a confident lock first, got %v", enrich.sequence())
	}
	var detour [][2]float64
	for r := loopRadius; r <= loopRadius+120; r += 4 { // straight out 120 m from the loop ...
		detour = append(detour, [2]float64{r, 0})
	}
	for r := loopRadius + 120; r >= loopRadius; r -= 4 { // ... and back
		detour = append(detour, [2]float64{r, 0})
	}
	feedFrames(d, "s1", detour)
	feedFrames(d, "s1", lapFraction(0.5))
	d.OnSessionEnd(rec)
	if enrich.callCount() != calls || enrich.clears != clears {
		t.Fatalf("lock must be kept: calls %d->%d, clears %d->%d", calls, enrich.callCount(), clears, enrich.clears)
	}
	if slug, conf := d.Track("s1"); slug != "" || conf != "" {
		t.Errorf("Track after session end = %q/%q, want none", slug, conf)
	}
}

// TestTrackDetector_ReleasesWhenPersistentlyOff: a lock is dropped once the car has driven
// releaseAfterM continuously more than releaseDistM from the locked layout (here onto a different,
// larger loop), the track is cleared, and a new detection stint locks the new layout.
func TestTrackDetector_ReleasesWhenPersistentlyOff(t *testing.T) {
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength},
		{Slug: "track-c", Points: circlePoints(0, 0, 700, 840), LengthM: 2 * math.Pi * 700},
		farTrack(),
	}
	enrich := &fakeEnricher{}
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, enrich, nil)
	var locks []string
	d.OnLock = func(_ context.Context, id string, _ processing.TimeSpan, slug string) error {
		locks = append(locks, slug)
		return nil
	}
	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	feedFrames(d, "s1", lapFraction(0.95))
	if slug, conf := d.Track("s1"); slug != "track-a" || conf != "confident" {
		t.Fatalf("Track = %q/%q, want track-a/confident", slug, conf)
	}
	clears := enrich.clears
	feedFrames(d, "s1", jitterPoints(circlePoints(0, 0, 700, 840)))
	d.OnSessionEnd(rec)
	if enrich.clears != clears+1 {
		t.Errorf("clears %d->%d, want exactly one release", clears, enrich.clears)
	}
	if got := enrich.lastCall(); got.slug != "track-c" || got.confidence != "confident" {
		t.Errorf("after release: last = %+v (seq %v), want track-c/confident", got, enrich.sequence())
	}
	if len(locks) != 2 || locks[0] != "track-a" || locks[1] != "track-c" {
		t.Errorf("OnLock = %v, want [track-a track-c]", locks)
	}
}

// TestTrackDetector_TwoTracksInOneSession_LabelsEachStint concatenates two different tracks in one
// session (with a teleport between them, as a track change does) and runs the real segmenter + lap
// store with the detector wired the way the backend wires it: every lap of each stint ends up labelled
// with its own track, confident, and no lap of the second stint inherits the first stint's track.
func TestTrackDetector_TwoTracksInOneSession_LabelsEachStint(t *testing.T) {
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength},
		{Slug: "track-b", Points: circlePoints(20000, 0, 400, 480), LengthM: 2 * math.Pi * 400},
		farTrack(),
	}
	store := processing.NewMemLapStore()
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, &fakeEnricher{}, nil)
	d.OnLock = store.LabelTrack
	seg := processing.NewLapSegmenter(store)
	seg.TrackFn = d.Track

	loopA := jitterPoints(circlePoints(0, 0, loopRadius, loopN))
	loopB := jitterPoints(circlePoints(20000, 0, 400, 480))
	type leg struct {
		lap int32
		pts [][2]float64
	}
	legs := []leg{
		{0, loopA[:100]}, // short first lap: completes before the lock, labelled by the backfill
		{1, loopA[100:]}, {2, loopA}, {3, loopA[:100]},
		{4, loopB[:100]}, // teleport onto B: the release fires part-way through this lap
		{5, loopB}, {6, loopB}, {7, loopB[:50]},
	}
	rec := &session.Record{ID: "s1"}
	seg.OnSessionStart(rec)
	d.OnSessionStart(rec)
	base := time.Unix(1_700_000_000, 0)
	n := 0
	for _, l := range legs {
		for _, p := range l.pts {
			f := posFrame(true, float32(p[0]), float32(p[1]), l.lap, uint32(n))
			f.RecvTimestamp = timestamppb.New(base.Add(time.Duration(n) * time.Second / 60))
			f.LastLap = durationpb.New(time.Minute)
			n++
			seg.OnFrame("s1", f) // segmenter before detector: the backend's sink order
			d.OnFrame("s1", f)
		}
	}
	seg.OnSessionEnd(rec)
	d.OnSessionEnd(rec)

	want := map[int32]string{0: "track-a", 1: "track-a", 2: "track-a", 3: "track-a", 5: "track-b", 6: "track-b", 7: "track-b"}
	for _, l := range store.BySession("s1") {
		if l.LapNumber == 4 { // spans the release: must at least not carry the first stint's track
			if l.TrackSlug == "track-a" {
				t.Errorf("lap 4 (driven on B) labelled %q/%q", l.TrackSlug, l.TrackConfidence)
			}
			continue
		}
		if l.TrackSlug != want[l.LapNumber] || l.TrackConfidence != "confident" {
			t.Errorf("lap %d = %q/%q, want %q/confident", l.LapNumber, l.TrackSlug, l.TrackConfidence, want[l.LapNumber])
		}
	}
}

// TestTrackDetector_SwitchesProvisionalGuess: two layouts of one venue (same Base, so no early margin
// lock): track X's reference covers only the first quarter of the loop, densely (so it fits the start
// best); track Y's covers the whole loop, sparsely (~13 m mean fit). The early provisional guess is X;
// as the rest of the lap is driven X stops fitting, the guess switches to Y, and Y locks on coverage.
func TestTrackDetector_SwitchesProvisionalGuess(t *testing.T) {
	loop := circlePoints(0, 0, loopRadius, loopN)
	arc := append(append([][2]float64(nil), loop[:loopN/4]...), [2]float64{-600, -600}, [2]float64{600, 600})
	geoms := []rkb.TrackGeom{
		{Slug: "track-x", Base: 3, Points: arc, LengthM: loopLength},
		{Slug: "track-y", Base: 3, Points: circlePoints(0, 0, loopRadius, 60), LengthM: loopLength},
	}
	got := runSession(t, geoms, lapFraction(0.95)).sequence()
	want := []string{"track-x/provisional", "track-y/provisional", "track-y/confident"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

// TestTrackDetector_NoLockWhileOffTheCandidate is the brands-hatch-indy-inside-the-GP case: layout S
// shares 70% of the driven loop and closes with a short-cut chord the car never takes, and the venue's
// other layout Y has poor reference geometry. S stays the best guess on cumulative fit, and the car
// does travel 0.9 of S's length - but by then it is ~200 m off S, so S must not lock.
func TestTrackDetector_NoLockWhileOffTheCandidate(t *testing.T) {
	loop := circlePoints(0, 0, loopRadius, loopN)
	arcEnd := loop[loopN*7/10]
	short := append([][2]float64(nil), loop[:loopN*7/10]...)
	for i := 1; i < 100; i++ { // chord from the arc's end back to its start
		f := float64(i) / 100
		short = append(short, [2]float64{arcEnd[0] + f*(loop[0][0]-arcEnd[0]), arcEnd[1] + f*(loop[0][1]-arcEnd[1])})
	}
	chord := math.Hypot(arcEnd[0]-loop[0][0], arcEnd[1]-loop[0][1])
	poor := circlePoints(0, 70, loopRadius, 60) // same venue, reference ~45 m off the driven line
	geoms := []rkb.TrackGeom{
		{Slug: "track-s", Base: 5, Points: short, LengthM: 0.7*loopLength + chord},
		{Slug: "track-y", Base: 5, Points: poor, LengthM: loopLength},
	}
	for _, c := range runSession(t, geoms, lapFraction(0.9)).sequence() {
		if c == "track-s/confident" {
			t.Fatal("track-s locked while the car was far off it")
		}
	}
}

// TestTrackDetector_WithdrawsUnsupportedGuess: a provisional guess is cleared once the car is driven
// somewhere no candidate fits at all (best mean > sanityCap), rather than left up indefinitely.
func TestTrackDetector_WithdrawsUnsupportedGuess(t *testing.T) {
	loop := circlePoints(0, 0, loopRadius, loopN)
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: loop[:loopN/8], LengthM: loopLength}, // no bbox: never pruned
		farTrack(),
	}
	enrich := &fakeEnricher{}
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, enrich, nil)
	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	clearsAtStart := enrich.clears
	feedFrames(d, "s1", jitterPoints(loop[:loopN/10]))
	if enrich.callCount() != 1 || enrich.lastCall().confidence != "provisional" {
		t.Fatalf("want one provisional guess, got %v", enrich.sequence())
	}
	feedFrames(d, "s1", jitterPoints(loop[loopN/4:])) // the rest of the loop: far from track-a's arc
	if enrich.clears != clearsAtStart+1 {
		t.Fatalf("want the unsupported provisional guess withdrawn (1 ClearTrack), got %d", enrich.clears-clearsAtStart)
	}
}

// TestTrackDetector_AmbiguousStaysNil feeds a lap of track A against [A, C] where C is an identical
// point cloud with no forward/reverse pairing: the two can never be told apart, so nothing is published.
func TestTrackDetector_AmbiguousStaysNil(t *testing.T) {
	loop := circlePoints(0, 0, loopRadius, loopN)
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: loop, LengthM: loopLength},
		{Slug: "track-c", Points: append([][2]float64(nil), loop...), LengthM: loopLength},
	}
	if got := runSession(t, geoms, lapFraction(0.95)).callCount(); got != 0 {
		t.Fatalf("expected no match between identical tracks, got %d calls", got)
	}
}

// TestTrackDetector_PartialCoverageStaysNil feeds fewer frames than minCoveragePoints: no guess at all.
func TestTrackDetector_PartialCoverageStaysNil(t *testing.T) {
	geoms := []rkb.TrackGeom{{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength}}
	if got := runSession(t, geoms, lapFraction(0.03)).callCount(); got != 0 { // 18 frames
		t.Fatalf("expected no match below the coverage threshold, got %d calls", got)
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
// (tools/rkb/rkb/reverse.py's _derive_corners reverses the forward's corner list). trusted sets
// ApexOrderTrusted on both (kinematic / derived-reverse corners: turn order = direction of travel).
func twinGeoms(boundary, apexes [][2]float64, trusted bool) []rkb.TrackGeom {
	forwardPoints := append(append([][2]float64(nil), boundary...), apexes...)
	reversedApexes := reversePoints(apexes)
	reverseLayoutPoints := append(append([][2]float64(nil), boundary...), reversedApexes...)
	return []rkb.TrackGeom{
		{Slug: "track-a", Base: 1, IsReverse: false, Points: forwardPoints, LengthM: loopLength,
			Apexes: apexes, ApexOrderTrusted: trusted},
		{Slug: "track-a-reverse", Base: 1, IsReverse: true, Points: reverseLayoutPoints, LengthM: loopLength,
			Apexes: reversedApexes, ApexOrderTrusted: trusted},
	}
}

// loopTwins builds the twin pair over the synthetic loop with 8 evenly spaced apexes.
func loopTwins(trusted bool) []rkb.TrackGeom {
	return twinGeoms(circlePoints(0, 0, loopRadius, loopN), circlePoints(0, 0, loopRadius, 8), trusted)
}

// lastSlug returns the last published slug and confidence, failing if nothing was published.
func lastSlug(t *testing.T, e *fakeEnricher) (string, string) {
	t.Helper()
	if e.callCount() == 0 {
		t.Fatal("expected a published match, got none")
	}
	c := e.lastCall()
	return c.slug, c.confidence
}

// TestTrackDetector_DirectionPicksTwin_Forward / _Reverse: an identical-geometry forward/reverse pair
// whose apex order is trusted is told apart by the direction of travel - driving it forward locks the
// forward layout, driving it backward locks the reverse layout.
func TestTrackDetector_DirectionPicksTwin_Forward(t *testing.T) {
	slug, conf := lastSlug(t, runSession(t, loopTwins(true), lapFraction(0.95)))
	if slug != "track-a" || conf != "confident" {
		t.Fatalf("forward drive: want track-a/confident, got %s/%s", slug, conf)
	}
}

func TestTrackDetector_DirectionPicksTwin_Reverse(t *testing.T) {
	slug, conf := lastSlug(t, runSession(t, loopTwins(true), reversePoints(lapFraction(0.95))))
	if slug != "track-a-reverse" || conf != "confident" {
		t.Fatalf("reverse drive: want track-a-reverse/confident, got %s/%s", slug, conf)
	}
}

// TestTrackDetector_UndirectedTwinFallsBackToForward: when the twins' apex order is NOT trusted
// (stored / outline-curvature corners), direction cannot be read, so the last-resort rule picks the
// forward layout whichever way it is driven.
func TestTrackDetector_UndirectedTwinFallsBackToForward(t *testing.T) {
	for name, pts := range map[string][][2]float64{
		"forward": lapFraction(0.95),
		"reverse": reversePoints(lapFraction(0.95)),
	} {
		slug, conf := lastSlug(t, runSession(t, loopTwins(false), pts))
		if slug != "track-a" || conf != "confident" {
			t.Fatalf("%s drive, untrusted apex order: want track-a/confident, got %s/%s", name, slug, conf)
		}
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

// TestTrackDetector_NonIdenticalTieStaysAmbiguous: two DIFFERENT layouts (same Base, opposite
// IsReverse, geometry a few cm apart - not twins) that tie on shape and carry no trusted direction
// stay ambiguous: nothing is published.
func TestTrackDetector_NonIdenticalTieStaysAmbiguous(t *testing.T) {
	loop := circlePoints(0, 0, loopRadius, loopN)
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Base: 1, IsReverse: false, Points: loop, LengthM: loopLength},
		{Slug: "track-d", Base: 1, IsReverse: true, Points: nudgePoints(loop, 0.02), LengthM: loopLength},
	}
	if got := runSession(t, geoms, lapFraction(0.95)).callCount(); got != 0 {
		t.Fatalf("expected non-identical tie to stay ambiguous, got %d calls", got)
	}
}

// TestTrackDetector_DirectionSeparatesNearIdenticalLayouts is the sardegna-road-a vs a2 case: two
// different but nearly coincident reference lines for the same road driven in opposite directions
// (trusted apex order), which shape alone cannot separate. Driving one way must lock that layout.
func TestTrackDetector_DirectionSeparatesNearIdenticalLayouts(t *testing.T) {
	loop := circlePoints(0, 0, loopRadius, loopN)
	apexes := circlePoints(0, 0, loopRadius, 8)
	geoms := []rkb.TrackGeom{
		{Slug: "road-a", Base: 1, Points: loop, LengthM: loopLength, Apexes: apexes, ApexOrderTrusted: true},
		{Slug: "road-a2", Base: 1, IsReverse: true, Points: nudgePoints(loop, 0.5), LengthM: loopLength,
			Apexes: reversePoints(apexes), ApexOrderTrusted: true},
	}
	if slug, conf := lastSlug(t, runSession(t, geoms, reversePoints(lapFraction(0.95)))); slug != "road-a2" || conf != "confident" {
		t.Fatalf("reverse drive: want road-a2/confident, got %s/%s", slug, conf)
	}
	if slug, conf := lastSlug(t, runSession(t, geoms, lapFraction(0.95))); slug != "road-a" || conf != "confident" {
		t.Fatalf("forward drive: want road-a/confident, got %s/%s", slug, conf)
	}
}

// TestTrackDetector_SessionEndWithdrawsProvisional: a session ending before any lock rule is met
// withdraws its provisional guess from the published track (it was never confirmed, and the stored lap
// labels are cleared with it), so the just-ended session's analysis cannot use a possibly wrong layout.
func TestTrackDetector_SessionEndWithdrawsProvisional(t *testing.T) {
	loop := circlePoints(0, 0, loopRadius, loopN)
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Base: 7, Points: loop, LengthM: loopLength},
		{Slug: "track-a-long", Base: 7, Points: circlePoints(0, 0, 600, 120), LengthM: 2 * math.Pi * 600},
	}
	enrich := runSession(t, geoms, lapFraction(0.4))
	if slug, conf := lastSlug(t, enrich); slug != "track-a" || conf != "provisional" {
		t.Fatalf("setup: want track-a/provisional published, got %s/%s", slug, conf)
	}
	if enrich.clears != 2 { // one at session start, one withdrawing the provisional guess at the end
		t.Fatalf("ClearTrack calls = %d, want 2 (start + end withdrawal)", enrich.clears)
	}
}

// TestBboxContains validates the bbox containment pre-filter used in the detector: driven bbox must
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

// TestTrackDetector_IgnoresPausedAndLoadingFrames: a paused or loading frame is not driving, even
// though its lap counter says on-track, so it must not count at all.
func TestTrackDetector_IgnoresPausedAndLoadingFrames(t *testing.T) {
	d := processing.NewTrackDetector(&fakeGeomProvider{}, &fakeEnricher{}, nil)
	d.OnSessionStart(&session.Record{ID: "s1"})
	paused := posFrame(true, 1, 2, 0, 1)
	paused.IsPaused = true
	loading := posFrame(true, 3, 4, 0, 2)
	loading.IsLoading = true
	d.OnFrame("s1", paused)
	d.OnFrame("s1", loading)
	if got := d.PointCountForTest("s1"); got != 0 {
		t.Fatalf("paused/loading frames must be ignored, got %d counted", got)
	}
}

// TestTrackDetector_StationaryFramesAddNoSamples: sitting on the grid or in the pit box for minutes
// must not flood the score with one repeated point - samples are taken by distance driven.
func TestTrackDetector_StationaryFramesAddNoSamples(t *testing.T) {
	d := processing.NewTrackDetector(&fakeGeomProvider{}, &fakeEnricher{}, nil)
	d.OnSessionStart(&session.Record{ID: "s1"})
	for i := 0; i < 600; i++ { // 10 s parked
		d.OnFrame("s1", posFrame(true, 500, 0, 0, uint32(i)))
	}
	if got := d.SampleCountForTest("s1"); got > 1 {
		t.Fatalf("parked frames must add at most one sample, got %d", got)
	}
	feedFrames(d, "s1", lapFraction(0.2)) // ~630 m driven
	if got := d.SampleCountForTest("s1"); got < 50 {
		t.Fatalf("driving ~630 m should add samples by distance, got %d", got)
	}
}

// TestTrackDetector_TeleportFrameDoesNotEliminateTrueTrack: one wild position (a stream glitch or
// reset) must not stretch the driven bbox past the true track's bbox and rule it out for good.
func TestTrackDetector_TeleportFrameDoesNotEliminateTrueTrack(t *testing.T) {
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength,
			HasBbox: true, Bbox: [4]float64{-loopRadius, -loopRadius, loopRadius, loopRadius}},
		farTrack(),
	}
	lap := lapFraction(0.95)
	pts := append(append(append([][2]float64(nil), lap[:100]...), [2]float64{90000, 90000}), lap[100:]...)
	if slug, conf := lastSlug(t, runSession(t, geoms, pts)); slug != "track-a" || conf != "confident" {
		t.Fatalf("want track-a/confident despite a teleport frame, got %s/%s", slug, conf)
	}
}

// TestTrackDetector_NonFinitePositionsCannotPanic: NaN/Inf positions are dropped on the ingest
// goroutine - no panic, no poisoned scores (the lap still locks).
func TestTrackDetector_NonFinitePositionsCannotPanic(t *testing.T) {
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Base: 1, Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength},
		farTrack(),
	}
	nan := float32(math.NaN())
	enrich := &fakeEnricher{}
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, enrich, nil)
	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	for i := 0; i < 100; i++ { // a session that starts with nothing but garbage
		d.OnFrame("s1", posFrame(true, nan, float32(math.Inf(1)), 0, uint32(i)))
	}
	for i, p := range lapFraction(0.95) {
		d.OnFrame("s1", posFrame(true, float32(p[0]), float32(p[1]), 0, uint32(i)))
		d.OnFrame("s1", posFrame(true, nan, nan, 0, uint32(i)))
	}
	d.OnSessionEnd(rec)
	if slug, conf := lastSlug(t, enrich); slug != "track-a" || conf != "confident" {
		t.Fatalf("want track-a/confident with NaN frames interleaved, got %s/%s", slug, conf)
	}
}

// TestTrackDetector_DirectedTwinWaitsForDirection: when both twins' apex order is trusted, the
// prefer-forward fallback is not used - nothing is published until direction picks one.
func TestTrackDetector_DirectedTwinWaitsForDirection(t *testing.T) {
	if got := runSession(t, loopTwins(true), reversePoints(lapFraction(0.95))[:loopN/8]).callCount(); got != 0 {
		t.Fatalf("trusted twins before direction is known: want no guess, got %d calls", got)
	}
}

// TestTrackDetector_SlowOnLockDoesNotHoldTheSession: OnLock (Postgres I/O in production) runs after
// the session state is unlocked, so Track() and OnFrame for the session are not blocked while it runs,
// and the lock is already published when it is called.
func TestTrackDetector_SlowOnLockDoesNotHoldTheSession(t *testing.T) {
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength},
		farTrack(),
	}
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, &fakeEnricher{}, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	d.OnLock = func(context.Context, string, processing.TimeSpan, string) error {
		close(entered)
		<-release
		return nil
	}
	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	done := make(chan struct{})
	go func() { feedFrames(d, "s1", lapFraction(0.95)); close(done) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("OnLock never called")
	}
	probe := make(chan string)
	go func() {
		d.OnFrame("s1", posFrame(true, loopRadius, 0, 0, 9999))
		slug, conf := d.Track("s1")
		probe <- slug + "/" + conf
	}()
	select {
	case got := <-probe:
		if got != "track-a/confident" {
			t.Errorf("Track during OnLock = %s, want track-a/confident", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Track/OnFrame blocked by a slow OnLock")
	}
	close(release)
	<-done
}

// endToEndLabels drives legs through the real segmenter + lap store + detector wired as the backend
// wires them (segmenter before detector; OnLock backfills; OnEnd clears provisional labels outside the
// spans whose backfill failed). labelFailures is how many LabelTrack calls fail first (-1: all of them).
// It returns the stored laps by lap number as they were just before OnEnd's clear ran (before) and
// after the session ended (after).
func endToEndLabels(t *testing.T, geoms []rkb.TrackGeom, legs []legPoints, labelFailures int) (before, after map[int32]*processing.Lap) {
	t.Helper()
	store := processing.NewMemLapStore()
	snapshot := func() map[int32]*processing.Lap {
		out := map[int32]*processing.Lap{}
		for _, l := range store.BySession("s1") {
			out[l.LapNumber] = l
		}
		return out
	}
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, &fakeEnricher{}, nil)
	failures := labelFailures
	d.OnLock = func(ctx context.Context, id string, span processing.TimeSpan, slug string) error {
		if failures != 0 {
			if failures > 0 {
				failures--
			}
			return errors.New("label failed")
		}
		return store.LabelTrack(ctx, id, span, slug)
	}
	d.OnEnd = func(ctx context.Context, id string, keep []processing.TimeSpan) {
		before = snapshot()
		if err := store.ClearProvisionalLabels(ctx, id, keep...); err != nil {
			t.Error(err)
		}
	}
	seg := processing.NewLapSegmenter(store)
	seg.TrackFn = d.Track
	rec := &session.Record{ID: "s1"}
	seg.OnSessionStart(rec)
	d.OnSessionStart(rec)
	base := time.Unix(1_700_000_000, 0)
	n := 0
	for _, l := range legs {
		for _, p := range l.pts {
			f := posFrame(true, float32(p[0]), float32(p[1]), l.lap, uint32(n))
			f.RecvTimestamp = timestamppb.New(base.Add(time.Duration(n) * time.Second / 60))
			f.LastLap = durationpb.New(time.Minute)
			n++
			seg.OnFrame("s1", f)
			d.OnFrame("s1", f)
		}
	}
	seg.OnSessionEnd(rec)
	d.OnSessionEnd(rec)
	return before, snapshot()
}

// legPoints is the points driven while the game's lap counter reads lap.
type legPoints struct {
	lap int32
	pts [][2]float64
}

// TestTrackDetector_NeverLocked_ClearsProvisionalAtEnd: a session that never confirms a track keeps no
// provisional label once it ends (the guess was often wrong: HSR-reverse as another layout).
func TestTrackDetector_NeverLocked_ClearsProvisionalAtEnd(t *testing.T) {
	// A quarter of a loop is below the coverage a lock needs but fits track-a as a provisional guess.
	loop := jitterPoints(circlePoints(0, 0, loopRadius, loopN))
	geoms := []rkb.TrackGeom{{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength}, farTrack()}
	before, after := endToEndLabels(t, geoms, []legPoints{{0, loop[:60]}, {1, loop[60:120]}, {2, loop[120:150]}}, 0)
	provisional := 0
	for _, l := range before {
		if l.TrackConfidence == processing.ConfidenceProvisional {
			provisional++
		}
	}
	if provisional == 0 {
		t.Fatalf("setup: no provisional lap before the end: %+v", before)
	}
	for n, l := range after {
		if l.TrackSlug != "" || l.TrackConfidence != "" {
			t.Errorf("lap %d = %q/%q after end, want none", n, l.TrackSlug, l.TrackConfidence)
		}
	}
}

// TestTrackDetector_Locked_KeepsConfidentAtEnd: a session that locks keeps its confident labels.
func TestTrackDetector_Locked_KeepsConfidentAtEnd(t *testing.T) {
	loop := jitterPoints(circlePoints(0, 0, loopRadius, loopN))
	geoms := []rkb.TrackGeom{{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength}, farTrack()}
	_, after := endToEndLabels(t, geoms, []legPoints{{0, loop[:100]}, {1, loop[100:]}, {2, loop}, {3, loop[:100]}}, 0)
	if len(after) == 0 {
		t.Fatal("no laps stored")
	}
	for n, l := range after {
		if l.TrackSlug != "track-a" || l.TrackConfidence != processing.ConfidenceConfident {
			t.Errorf("lap %d = %q/%q, want track-a/confident", n, l.TrackSlug, l.TrackConfidence)
		}
	}
}

// TestTrackDetector_SecondStintNeverLocks_KeepsFirstClearsSecond: stint 1 locks track-a and keeps it;
// after the release, stint 2 (a path matching nothing for long enough to lock) is cleared at the end.
func TestTrackDetector_SecondStintNeverLocks_KeepsFirstClearsSecond(t *testing.T) {
	loopA := jitterPoints(circlePoints(0, 0, loopRadius, loopN))
	// track-b's reference is only the first quarter of the second loop, so driving it never completes
	// enough coverage to lock but does fit as a provisional guess.
	loopB := circlePoints(20000, 0, 400, 480)
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength},
		{Slug: "track-b", Points: circlePoints(20000, 0, 400, 480), LengthM: 2 * math.Pi * 400},
		farTrack(),
	}
	jb := jitterPoints(loopB)
	before, after := endToEndLabels(t, geoms, []legPoints{
		{0, loopA[:100]}, {1, loopA[100:]}, {2, loopA}, {3, loopA[:100]},
		{4, jb[:100]}, {5, jb[100:140]}, {6, jb[140:170]},
	}, 0)
	sawProvisional := false
	for _, l := range before {
		sawProvisional = sawProvisional || l.TrackConfidence == processing.ConfidenceProvisional
	}
	if !sawProvisional {
		t.Fatalf("setup: second stint left no provisional lap: %+v", before)
	}
	for n, l := range after {
		if n <= 2 && (l.TrackSlug != "track-a" || l.TrackConfidence != processing.ConfidenceConfident) {
			t.Errorf("stint 1 lap %d = %q/%q, want track-a/confident", n, l.TrackSlug, l.TrackConfidence)
		}
		if n >= 5 && (l.TrackSlug != "" || l.TrackConfidence != "") {
			t.Errorf("stint 2 lap %d = %q/%q, want none", n, l.TrackSlug, l.TrackConfidence)
		}
	}
}

// twoStintLegs is a session whose first stint locks track-a and whose second never locks track-b.
func twoStintLegs() ([]rkb.TrackGeom, []legPoints) {
	loopA := jitterPoints(circlePoints(0, 0, loopRadius, loopN))
	jb := jitterPoints(circlePoints(20000, 0, 400, 480))
	geoms := []rkb.TrackGeom{
		{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength},
		{Slug: "track-b", Points: circlePoints(20000, 0, 400, 480), LengthM: 2 * math.Pi * 400},
		farTrack(),
	}
	return geoms, []legPoints{
		{0, loopA[:100]}, {1, loopA[100:]}, {2, loopA}, {3, loopA[:100]},
		{4, jb[:100]}, {5, jb[100:140]}, {6, jb[140:170]},
	}
}

// TestTrackDetector_BackfillFailsOnce_RetriedAtEnd: the lock's backfill fails once; the detector retries
// it at session end BEFORE clearing, so the laps detection confirmed end up confident, not unlabelled.
func TestTrackDetector_BackfillFailsOnce_RetriedAtEnd(t *testing.T) {
	geoms, legs := twoStintLegs()
	_, after := endToEndLabels(t, geoms, legs, 1)
	for n := int32(0); n <= 3; n++ {
		if l := after[n]; l.TrackSlug != "track-a" || l.TrackConfidence != processing.ConfidenceConfident {
			t.Errorf("stint 1 lap %d = %q/%q, want track-a/confident", n, l.TrackSlug, l.TrackConfidence)
		}
	}
	// The retry is bounded to stint 1: the never-locked stint 2 must be cleared, not labelled track-a.
	sawStint2 := false
	for n, l := range after {
		if n >= 5 {
			sawStint2 = true
			if l.TrackSlug != "" || l.TrackConfidence != "" {
				t.Errorf("stint 2 lap %d = %q/%q, want none", n, l.TrackSlug, l.TrackConfidence)
			}
		}
	}
	if !sawStint2 {
		t.Fatal("setup: no stint 2 laps")
	}
}

// TestTrackDetector_BackfillAlwaysFails_KeepsLockedStintClearsOther: the backfill never succeeds, so the
// locked stint's provisional laps (right, just not promoted) are kept; the never-locked stint is cleared.
func TestTrackDetector_BackfillAlwaysFails_KeepsLockedStintClearsOther(t *testing.T) {
	geoms, legs := twoStintLegs()
	before, after := endToEndLabels(t, geoms, legs, -1)
	if before[0] == nil || before[0].TrackConfidence != processing.ConfidenceProvisional {
		t.Fatalf("setup: lap 0 should be provisional before the end: %+v", before[0])
	}
	for n := int32(0); n <= 3; n++ {
		if after[n].TrackSlug != before[n].TrackSlug || after[n].TrackConfidence != before[n].TrackConfidence {
			t.Errorf("stint 1 lap %d changed %q/%q -> %q/%q, want kept", n,
				before[n].TrackSlug, before[n].TrackConfidence, after[n].TrackSlug, after[n].TrackConfidence)
		}
	}
	sawStint2 := false
	for n, l := range after {
		if n >= 5 {
			sawStint2 = true
			if l.TrackSlug != "" || l.TrackConfidence != "" {
				t.Errorf("stint 2 lap %d = %q/%q, want none", n, l.TrackSlug, l.TrackConfidence)
			}
		}
	}
	if !sawStint2 {
		t.Fatal("setup: no stint 2 laps")
	}
}

// TestTrackDetector_SessionEndDBHangIsBounded: a lock backfill retry and a clear that hang (a stuck
// database) are cut off by the session-end timeout: OnSessionEnd returns within the bound, the timed-out
// retry counts as failed (its span is handed to the clear as kept), and the hung clear is just abandoned.
func TestTrackDetector_SessionEndDBHangIsBounded(t *testing.T) {
	geoms, legs := twoStintLegs()
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, &fakeEnricher{}, nil)
	const timeout = 100 * time.Millisecond
	processing.SetEndTimeoutForTest(d, timeout)
	calls := 0
	d.OnLock = func(ctx context.Context, id string, span processing.TimeSpan, slug string) error {
		calls++
		if calls == 1 {
			return errors.New("label failed") // the mid-session backfill fails fast
		}
		<-ctx.Done() // the session-end retry hangs until its deadline
		return ctx.Err()
	}
	var keep []processing.TimeSpan
	clearReturned := false
	d.OnEnd = func(ctx context.Context, id string, k []processing.TimeSpan) {
		keep = k
		<-ctx.Done() // the clear hangs too
		clearReturned = true
	}
	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)
	for i, l := range legs {
		for j, p := range l.pts {
			d.OnFrame("s1", posFrame(true, float32(p[0]), float32(p[1]), l.lap, uint32(i*1000+j)))
		}
	}
	start := time.Now()
	d.OnSessionEnd(rec)
	if took := time.Since(start); took > 10*timeout {
		t.Errorf("OnSessionEnd took %v with a %v timeout", took, timeout)
	}
	if !clearReturned {
		t.Error("clear was not cut off by its deadline")
	}
	if len(keep) != 1 || !keep[0].From.IsZero() {
		t.Errorf("keep = %+v, want the failed stint's span", keep)
	}
}

// TestTrackDetector_LockBackfillHangIsBounded: a lock backfill (OnLock) whose database call hangs must
// not stall the ingest goroutine. fireLock bounds it like the session-end calls, and a timed-out backfill
// counts as failed: it is retried at session end and its span is kept rather than cleared.
func TestTrackDetector_LockBackfillHangIsBounded(t *testing.T) {
	geoms, legs := twoStintLegs()
	d := processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, &fakeEnricher{}, nil)
	const timeout = 100 * time.Millisecond
	processing.SetEndTimeoutForTest(d, timeout)
	d.OnLock = func(ctx context.Context, id string, span processing.TimeSpan, slug string) error {
		<-ctx.Done() // every backfill hangs until its deadline
		return ctx.Err()
	}
	var keep []processing.TimeSpan
	d.OnEnd = func(ctx context.Context, id string, k []processing.TimeSpan) { keep = k }
	rec := &session.Record{ID: "s1"}
	d.OnSessionStart(rec)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i, l := range legs {
			for j, p := range l.pts {
				d.OnFrame("s1", posFrame(true, float32(p[0]), float32(p[1]), l.lap, uint32(i*1000+j)))
			}
		}
		d.OnSessionEnd(rec)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hung lock backfill stalled OnFrame/OnSessionEnd (no deadline)")
	}
	if len(keep) != 1 || !keep[0].From.IsZero() {
		t.Errorf("keep = %+v, want the timed-out stint's span kept", keep)
	}
}

// blockingGeomProvider's DetectionGeometry blocks until its context is done.
type blockingGeomProvider struct{}

func (blockingGeomProvider) DetectionGeometry(ctx context.Context) ([]rkb.TrackGeom, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestTrackDetector_GeometryLoadHangIsBounded: the lazy geometry load runs inline on the ingest goroutine
// (OnSessionStart), so a stalled read must be cut off by its deadline: detection is then disabled for the
// run (logged), exactly like any other load failure, and the stream keeps flowing.
func TestTrackDetector_GeometryLoadHangIsBounded(t *testing.T) {
	d := processing.NewTrackDetector(blockingGeomProvider{}, &fakeEnricher{}, nil)
	processing.SetGeomTimeoutForTest(d, 100*time.Millisecond)
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.OnSessionStart(&session.Record{ID: "s1"})
		d.OnFrame("s1", posFrame(true, 1, 2, 1, 1))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a hung geometry load stalled OnSessionStart (no deadline)")
	}
}
