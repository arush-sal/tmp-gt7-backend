package processing

import (
	"context"
	"math"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// Tunable constants for the shape matcher. Values marked "tuned" were chosen by replaying the real
// captures (supporting-data/raw-packet-captures, captures/, the VictoryDash laps) through the detector;
// see the per-constant notes for the evidence.
const (
	// bboxTolerance widens a candidate's reference bbox (world metres) before the containment test: the
	// driven path is noisy, so requiring EXACT containment would spuriously reject a true match whose
	// noisy positions poke a few metres outside the reference track's boundary bbox. Tuned against the 11
	// real VictoryDash driven laps (Task 8): the worst-case poke of a true track's full-lap driven line
	// past its own RKB affine boundary bbox measured 6.4m (watkins-glen-long; deep-forest-2 5.2m). 10.0m
	// clears that with ~3.6m headroom so the correct candidate survives the pre-filter across the WHOLE
	// lap - not just an early partial-path window - while staying an order of magnitude below the
	// inter-track score separation (correct ~4m vs runner-up >50m), so widening it never admits a wrong
	// winner: it only adds far-away candidates that the shape score + margin gate then reject.
	bboxTolerance = 10.0

	// sampleStepM is the distance (world metres) the car must move from the last sampled point before
	// another is queued for scoring. Sampling by distance, not frame count, keeps the score a function of
	// the track driven: a car parked on the grid or in the pit box adds nothing (rather than piling one
	// repeated point onto every candidate until the scores tie), and scoring cost is bounded per metre.
	// 5 m equals the previous every-5th-frame stride at ~215 km/h.
	sampleStepM = 5.0

	// marginRatio is the maximum allowed ratio of the best candidate's score to the second-best's for the
	// match to be published at all, even provisionally (best/secondBest <= marginRatio): the winner must
	// be clearly better than the runner-up, or the two candidates are too similar to distinguish.
	marginRatio = 0.7

	// sanityCap is a loose, boundary-source-agnostic upper bound (world metres) on the winning mean
	// nearest-neighbour score. It exists only to reject "nothing fits" - even a lone surviving candidate
	// is rejected if its fit is this bad.
	sanityCap = 50.0

	// minCoveragePoints is the minimum number of on-track frames before any (provisional) guess is
	// published: too little of the lap driven so far cannot distinguish tracks by shape at all.
	minCoveragePoints = 30

	// reattemptEveryN is the re-scoring cadence in on-track frames (~4 Hz at 60 Hz): the newly sampled
	// points are folded into every live candidate's running score and the decision re-evaluated only
	// this often, never per frame.
	reattemptEveryN = 15

	// lockCoverageFraction locks the current best match once the car has travelled this fraction of
	// that candidate's official lap length (tracks.length_m) since detection started: by then the whole
	// lap's shape has been scored, which is what the full-lap harness showed separates every
	// capture-resolved track from its look-alikes. Below 1.0 because a driven racing line cuts corners
	// and is shorter than the official (centre-line) length. Tuned: at 0.9 every coverage lock on the
	// captures (brands-hatch-indy, grand-valley-south-2, red-bull-ring 1.30, special-stage-route-x) was
	// the true track.
	lockCoverageFraction = 0.9

	// lockMarginMaxMean, lockMarginRatio and lockMarginMinFraction are the EARLY lock (before full-lap
	// coverage): the best candidate's mean nearest-neighbour distance must be at most lockMarginMaxMean
	// metres AND the runner-up's at least lockMarginRatio times worse, after at least
	// lockMarginMinFraction of the best candidate's lap length has been driven (and with no same-venue
	// sibling layout still feasible, see siblingFeasible). Tuned on 19 capture replays: every true
	// margin lock had best 2.0-4.5 m with the runner-up 6.4-60x worse; the only wrong best candidate that
	// came close was brands-hatch-indy while driving the GP circuit (5.05 m, 11x), which the sibling
	// guard blocks; every other wrong best stayed >= 15 m or within 2.3x.
	lockMarginMaxMean     = 5.0
	lockMarginRatio       = 4.0
	lockMarginMinFraction = 0.3

	// lockRecentMaxM is the furthest (world metres) the car may currently be from the best candidate -
	// the latest re-score batch's mean distance - for either lock to fire. A layout contained in a
	// longer one (brands-hatch-indy inside the GP circuit) fits the shared part perfectly, so by the time
	// ~a lap's worth of its length is driven its cumulative mean can still look acceptable while the car
	// is already far out on the part it does not have; requiring a good fit right now stops that lock.
	// A pit lane or off-track excursion only delays a lock until the car is back on the layout.
	lockRecentMaxM = 25.0

	// maxStepM caps one frame-to-frame step counted toward the travelled distance (world metres): a
	// larger jump is a teleport (pit/garage reset, replay rewind, a gap in the stream), not driving.
	// At 60 Hz even 400 km/h is under 2 m/frame, so 50 m only ever trips on a discontinuity.
	maxStepM = 50.0

	// apexVisitRadius is how close (world metres) a sampled driven point must be to a corner apex to
	// count as visiting that corner, for the direction-of-travel check. Generous because kinematic apexes
	// sit on a racing line, not a centre line.
	apexVisitRadius = 40.0

	// minDirSteps is how many apex-to-apex steps AGAINST a candidate's turn order (and more than with it)
	// are needed before that candidate is ruled out as being driven the wrong way. Real opposite-direction
	// pairs on the captures showed 7-40 wrong-way steps vs 0-3 right-way ones per lap, so 3 rules a
	// wrong-direction layout out within a few corners while a stray step (pit exit, spin) cannot.
	minDirSteps = 3

	// releaseDistM and releaseAfterM release a lock (see TrackDetector.watchLock): once the car has
	// driven releaseAfterM metres in a row, every one of them more than releaseDistM from the locked
	// layout's reference points, the session has moved to another track (a track change inside one
	// session, a teleport followed by driving elsewhere) and detection starts a new stint. Any frame
	// back within releaseDistM resets the run, and teleports add no distance, so a rewind or replay on
	// the same track never counts. Tuned on the 19 capture replays (each fed as ONE session): the
	// furthest a locked car got from its own layout was 43.7 m (willow, a 62 m run beyond 25 m) and the
	// grand-valley-south-2 pit lane runs 515 m at 25-33 m off - so 25 m (lockRecentMaxM) would release
	// spuriously in the pit lane, while beyond 50 m no same-track capture drove a single metre. The
	// concatenated Sardegna A2 -> B2 -> C2 stream still releases and re-locks at every layout change.
	// 300 m releases within a few seconds of driving on a different layout.
	releaseDistM  = 50.0
	releaseAfterM = 300.0

	// twinGeometryEpsilon is the per-coordinate tolerance (world metres) used to decide whether two
	// candidates' point clouds are the "same" geometry for twin detection. A forward/reverse pair is
	// generated by copying the boundary+corner rows verbatim (see rtps/rkb DetectionGeometry), so a true
	// twin's points match to (near) machine precision; a merely similar but genuinely different layout
	// will not survive this tight a tolerance.
	twinGeometryEpsilon = 1e-6
)

// Track confidence values published via TrackEnricher.SetTrack.
const (
	// ConfidenceProvisional marks the current best guess while the detector is still re-scoring: it may
	// still switch to another track.
	ConfidenceProvisional = "provisional"
	// ConfidenceConfident marks a locked match (full-lap coverage or a clear margin); it is kept until the
	// car persistently drives off the layout (see releaseAfterM), which starts a new detection stint.
	ConfidenceConfident = "confident"
)

// DetectionGeomProvider is the narrow read-only seam TrackDetector needs from the RKB store: the
// lightweight per-track reference geometry used for live shape matching. Defined here (rather than
// depending on rkb.Store) so TrackDetector only requires what it uses; *rkb.SQLiteStore satisfies
// this structurally, with no import from rkb back into processing.
type DetectionGeomProvider interface {
	DetectionGeometry(ctx context.Context) ([]rkb.TrackGeom, error)
}

// TrackEnricher is the narrow seam TrackDetector needs from the live enrichment state: publishing or
// clearing the correlated track fields for a session. Defined here (rather than depending on the
// concrete *live.EnrichmentState) because rtps/live already imports rtps/processing (for lap/analysis
// types) - depending on rtps/live directly would create an import cycle. *live.EnrichmentState
// satisfies this interface structurally.
type TrackEnricher interface {
	// SetTrack publishes a resolved track match for sessionID.
	SetTrack(sessionID string, slug *string, confidence *string)
	// ClearTrack nils the track fields for sessionID (e.g. on a new session with no match yet).
	ClearTrack(sessionID string)
}

// candState is one candidate track's running evidence for one session: the summed nearest-neighbour
// distance of every sampled driven point (so the mean over everything driven so far is sum/samples,
// updated incrementally instead of re-scoring the whole path), whether it is still bbox-feasible, and
// its direction-of-travel tally.
type candState struct {
	alive bool    // false once the driven bbox pokes outside this candidate's bbox (never revives: the bbox only grows)
	sum   float64 // sum of nearest-neighbour distances (metres) over the session's sampled points
	// recent is the mean nearest-neighbour distance of the latest scored batch only: where the car is
	// NOW relative to this candidate, which the cumulative mean is too slow to show (see lockRecentMaxM).
	recent float64

	// Direction of travel (only when the candidate's apex order is trusted, see rkb.TrackGeom): the last
	// apex visited, and how many apex-to-apex steps went with (fwd) / against (back) its turn order.
	lastApex  int
	fwd, back int
}

// trackDetectState is one session's incremental detection state. It is owned by the session's stream
// goroutine (all Sink calls for one session arrive on it), and mu only guards against the test
// accessor; the hot path takes it uncontended.
type trackDetectState struct {
	mu sync.Mutex

	geoms  []rkb.TrackGeom // the detector's detectable geometry, captured at session start
	twinOf []int           // twinOf[i] = index of geoms[i]'s identical-geometry forward/reverse twin, or -1
	cands  []candState     // parallel to geoms

	frames     int          // on-track frames seen
	samples    int          // sampled points folded into every alive candidate's sum
	pending    [][2]float64 // sampled points not yet folded in (flushed every reattemptEveryN frames)
	bbox       [4]float64   // driven bbox over accepted positions (teleport frames excluded)
	hasBbox    bool         // bbox holds at least one accepted position
	travelled  float64      // metres driven since detection started (teleports excluded, see maxStepM)
	last       [2]float64   // previous finite position (the reference for the next step)
	hasLast    bool
	lastSample [2]float64 // last queued sample (see sampleStepM)
	hasSample  bool

	// published is what was last sent via SetTrack ("" = nothing yet), so SetTrack fires only on a change.
	publishedSlug, publishedConf string
	latched                      bool // a confident match was published: only watchLock runs until a release
	// user: the published track is the user's pick (TrackOverride, committed from the HTTP goroutine),
	// so detection is bypassed for the session: no scoring, no lock release. userSlug is the last pick
	// (kept after a withdrawal: its labels are what the withdrawal downgrades). userDirty: a pick or
	// withdrawal was committed and the stored labels are not yet relabelled (syncUser; atomic so the
	// per-frame check is lock-free). userRetry: that relabel failed and is retried at session end.
	user      bool
	userSlug  string
	userDirty atomic.Bool
	userRetry bool

	// locks is every lock detection made in the session, its span closed at the release, or at a pick
	// (so a withdrawal can give those laps back their confirmed label). lastRecv is the recv time of the
	// last frame detection saw, where a pick closes the open lock's span.
	locks    []lockSpan
	lastRecv time.Time

	locked     int       // geoms index of the latched match (valid while latched)
	offRunM    float64   // metres driven in a row more than releaseDistM from the locked layout
	stintStart time.Time // recv time of the frame that began this detection stint (zero: session start)

	// failed lists the locks whose OnLock backfill returned an error (rare: a storage failure), so
	// OnSessionEnd can retry them and, if still failing, protect those stints' laps from the clear.
	failed []lockSpan
}

// lockSpan is a lock whose backfill failed, with the stint's end (zero while the stint is still open).
type lockSpan struct {
	lockEvent
	until time.Time
}

// newStint resets the matching state for a fresh detection stint starting at start (zero time for the
// session's first stint). The reference position is kept so the next step is still vetted.
func (st *trackDetectState) newStint(start time.Time) {
	st.cands = make([]candState, len(st.geoms))
	for i := range st.cands {
		st.cands[i] = candState{alive: true, lastApex: -1}
	}
	st.frames, st.samples, st.pending = 0, 0, st.pending[:0]
	st.hasBbox, st.travelled, st.hasSample = false, 0, false
	st.publishedSlug, st.publishedConf, st.latched = "", "", false
	st.offRunM, st.stintStart = 0, start
}

// TrackDetector implements session.Sink: it keeps re-scoring the driven path of each session against
// the RKB's reference track geometry, publishing the current best match as "provisional" (it may
// switch) until either ~a full lap has been driven or the best match is clearly ahead, at which point
// it publishes "confident" and keeps that track until the car persistently drives off it (a track
// change within the session), which clears the track and starts a new detection stint. See decide for
// the matching rules and watchLock for the release.
//
// Sink methods are called on each stream's goroutine; d.mu guards only the session map and the
// lazily-loaded geometry, so one session's scoring never blocks another's.
type TrackDetector struct {
	geomProvider DetectionGeomProvider
	enrich       TrackEnricher
	log          *zap.Logger

	mu    sync.Mutex
	state map[string]*trackDetectState

	// geomLoaded, geoms and twinOf cache the detection geometry after its first (lazy) load: it is
	// loaded once per detector lifetime rather than per session, and the twin pairs (an O(n log n)
	// sort per pair) are computed once with it rather than on every decision.
	geomLoaded bool
	geoms      []rkb.TrackGeom
	twinOf     []int

	// OnLock, if set, is called (on the stream goroutine, once per stint) when a match locks: slug
	// locked for sessionID, for the stint that began at span.From (zero = the session start). The backend
	// backfills the stint's unlabelled/provisional laps with it (TrackRefStore.LabelTrack). It runs after
	// the lock is published (SetTrack) and outside the session's lock, so it may do I/O. span.Until is
	// zero (open-ended) except in OnSessionEnd's retry of an already-ended stint; the retry's ctx carries
	// the session-end timeout. A returned error marks the backfill failed (retried at session end).
	OnLock func(ctx context.Context, sessionID string, span TimeSpan, slug string) error

	// OnEnd, if set, is called once as the last step of OnSessionEnd (after the final re-score and its
	// OnLock backfill), with a ctx bounded by the session-end timeout; keep is the spans of stints whose
	// backfill still failed, whose provisional laps must not be cleared.
	OnEnd func(ctx context.Context, sessionID string, keep []TimeSpan)

	// Override, if set, is the user's track pick (see TrackOverride; NewTrackOverride sets it).
	Override *TrackOverride
	// OnUserTrack, if set, is called (on the stream goroutine, at the frame after a pick or withdrawal
	// was committed, or at the session end) with the pick's slug and confident, or with the withdrawn
	// slug and provisional. The backend sets every lap of the session to it
	// (TrackRefStore.SetSessionTrack) and reloads the stored reference. A failed call is retried once
	// at session end.
	OnUserTrack func(ctx context.Context, sessionID, slug, confidence string) error

	endTimeout  time.Duration // per-call bound on lock backfills, their retry and the label clear (endDBTimeout)
	geomTimeout time.Duration // bound on the one-time lazy geometry load (geomLoadTimeout)
}

// NewTrackDetector returns a detector that matches driven paths against geom's reference geometry and
// publishes results into enrich. A nil log defaults to zap.NewNop() (matches ingest.NewServer / api
// server / backend wiring convention).
func NewTrackDetector(geom DetectionGeomProvider, enrich TrackEnricher, log *zap.Logger) *TrackDetector {
	if log == nil {
		log = zap.NewNop()
	}
	return &TrackDetector{
		geomProvider: geom,
		enrich:       enrich,
		log:          log,
		state:        make(map[string]*trackDetectState),
		endTimeout:   endDBTimeout,
		geomTimeout:  geomLoadTimeout,
	}
}

// endDBTimeout bounds each Postgres call the detector makes on the ingest goroutine (every lock backfill,
// its session-end retry, and the label clear), so a hung database must not stall the stream and the next
// session; these are single indexed UPDATEs that take milliseconds, so 5 s is generous yet short enough to
// move on.
const endDBTimeout = 5 * time.Second

// geomLoadTimeout bounds the one-time lazy RKB geometry load (a local SQLite read, also on the ingest
// goroutine, in OnSessionStart). It is deliberately much looser than endDBTimeout: the load runs once per
// process and a timeout disables track detection until restart, so a briefly slow disk must not trip it,
// while a truly stalled read still can't hang ingest forever.
const geomLoadTimeout = 30 * time.Second

// OnSessionStart starts fresh detection for the session and clears any stale track (a new session has
// not been matched yet, even if the previous one was). It also lazily loads the reference geometry on
// the detector's first use; a load failure is logged and leaves geoms empty, so the session simply
// never matches rather than panicking or retrying every frame.
func (d *TrackDetector) OnSessionStart(rec *session.Record) {
	d.mu.Lock()
	if !d.geomLoaded {
		d.geomLoaded = true // attempt exactly once: a persistent load failure must not retry per session
		ctx, cancel := context.WithTimeout(context.Background(), d.geomTimeout)
		geoms, err := d.geomProvider.DetectionGeometry(ctx)
		cancel()
		if err != nil {
			d.log.Warn("track detection geometry load failed; track detection disabled for this run",
				zap.Error(err))
		} else {
			d.geoms = filterDetectable(geoms)
			d.twinOf = twinIndex(d.geoms)
		}
	}
	st := &trackDetectState{geoms: d.geoms, twinOf: d.twinOf}
	st.newStint(time.Time{})
	d.state[rec.ID] = st
	d.mu.Unlock()

	d.enrich.ClearTrack(rec.ID)
	if d.Override != nil {
		d.Override.start(rec.ID) // commits a pick made while no session was open
	}
}

// commitUser applies the user's pick (slug) or its withdrawal ("") to the open session id at once, on
// the caller's (HTTP) goroutine; the caller holds TrackOverride.mu, which the session's start and end
// also take, so the pick is the session's before the end can run. A pick wins silently over detection:
// it is published as confident (so the stored reference loads), Track returns it for later laps, and
// detection is bypassed for the rest of the session (frame returns early). A withdrawal clears the
// published track and restarts detection from the session start, so a later lock backfills the whole
// session. The stored labels follow on the ingest goroutine (syncUser). Reports whether anything
// changed (false: unknown session, or a withdrawal with no pick).
func (d *TrackDetector) commitUser(id, slug string) bool {
	st := d.session(id)
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if slug == "" {
		if !st.user {
			return false // nothing picked: detection already owns the session
		}
		st.user = false
		st.newStint(time.Time{})
		d.enrich.ClearTrack(id)
		d.log.Info("user track withdrawn; detecting again", zap.String("session_id", id), zap.String("slug", st.userSlug))
	} else {
		if !st.user && st.latched {
			// End the open lock's span just after the last frame detection saw: laps that began by then
			// were confirmed by it, later ones are the pick's. +1ns because TimeSpan is half-open.
			st.locks[len(st.locks)-1].until = st.lastRecv.Add(time.Nanosecond)
		}
		st.user, st.userSlug, st.failed = true, slug, nil // earlier backfill retries would fight the pick
		st.publishedSlug, st.publishedConf = slug, ConfidenceConfident
		conf := ConfidenceConfident
		d.enrich.SetTrack(id, &slug, &conf)
		d.log.Info("track set by the user", zap.String("session_id", id), zap.String("slug", slug))
	}
	st.userDirty.Store(true)
	return true
}

// syncUser brings the session's stored lap labels in line with the committed pick, on the ingest
// goroutine (storage I/O, so outside st.mu): a pick sets every lap of the session to it (OnUserTrack,
// confident); a withdrawal downgrades them to provisional and then re-confirms the laps of every lock
// detection made (OnLock over its span; LabelTrack only touches provisional laps), so only the laps
// the pick labelled are left for a later lock to relabel or the session end to clear. A failed
// relabel is retried when force (session end) is set; a failed lock restore is kept like a failed
// backfill (st.failed). Lock-free unless something was committed.
func (d *TrackDetector) syncUser(id string, st *trackDetectState, force bool) {
	if !force && !st.userDirty.Load() {
		return
	}
	st.mu.Lock()
	if !st.userDirty.Load() && (!force || !st.userRetry) {
		st.mu.Unlock()
		return
	}
	st.userDirty.Store(false)
	st.userRetry = false
	picked, slug, restore := st.user, st.userSlug, slices.Clone(st.locks)
	st.mu.Unlock()

	conf := ConfidenceConfident
	if !picked {
		conf = ConfidenceProvisional
	}
	if err := d.relabel(id, slug, conf); err != nil {
		st.mu.Lock()
		st.userRetry = true
		st.mu.Unlock()
		if force {
			d.log.Warn("relabelling laps for the user's track still failing at session end",
				zap.String("session_id", id), zap.String("slug", slug), zap.String("confidence", conf), zap.Error(err))
		}
		return
	}
	if picked || d.OnLock == nil {
		return
	}
	for _, l := range restore {
		ctx, cancel := context.WithTimeout(context.Background(), d.endTimeout)
		err := d.OnLock(ctx, id, TimeSpan{From: l.since, Until: l.until}, l.slug)
		cancel()
		if err != nil {
			st.mu.Lock()
			st.failed = append(st.failed, l)
			st.mu.Unlock()
		}
	}
}

// relabel calls OnUserTrack (if set), bounded by the end timeout like a lock backfill.
func (d *TrackDetector) relabel(sessionID, slug, conf string) error {
	if d.OnUserTrack == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), d.endTimeout)
	defer cancel()
	return d.OnUserTrack(ctx, sessionID, slug, conf)
}

// filterDetectable drops tracks with no reference points (the needs-capture tracks): they can never
// match, so keeping them around would only cost cycles in the scoring loop.
func filterDetectable(geoms []rkb.TrackGeom) []rkb.TrackGeom {
	out := make([]rkb.TrackGeom, 0, len(geoms))
	for _, g := range geoms {
		if len(g.Points) == 0 {
			continue
		}
		out = append(out, g)
	}
	return out
}

// twinIndex returns, for each geom, the index of its forward/reverse twin (see isTwin), or -1.
func twinIndex(geoms []rkb.TrackGeom) []int {
	out := make([]int, len(geoms))
	for i := range out {
		out[i] = -1
	}
	for i := range geoms {
		for j := i + 1; j < len(geoms); j++ {
			if out[i] < 0 && out[j] < 0 && isTwin(geoms[i], geoms[j]) {
				out[i], out[j] = j, i
			}
		}
	}
	return out
}

func (d *TrackDetector) session(id string) *trackDetectState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state[id]
}

// OnFrame folds one driving frame into the session's running state (O(1) per frame: bbox, travelled
// distance, and a position queued for scoring every sampleStepM metres). Every reattemptEveryN frames the
// queued points are scored against the live candidates and the decision re-evaluated. Ignored: off-track,
// paused and loading frames, non-finite positions, unknown or locked sessions. A teleport (a step over
// maxStepM, e.g. a reset or a glitched position) only moves the reference point: the frame itself never
// reaches the bbox or the samples, so one bad position cannot rule out the true track. The very first
// position is likewise only a reference, since there is no step yet to vet it by. While locked, only
// watchLock runs.
func (d *TrackDetector) OnFrame(sessionID string, f *telemetryv1.TelemetryFrame) {
	st := d.session(sessionID)
	if st == nil {
		return
	}
	d.syncUser(sessionID, st, false) // relabel for a committed pick; one atomic load otherwise
	if !session.OnTrack(f) || f.GetIsPaused() || f.GetIsLoading() {
		return
	}
	p := [2]float64{float64(f.GetPositionX()), float64(f.GetPositionZ())}
	if !finite(p[0]) || !finite(p[1]) {
		return // a NaN/Inf position would poison every score (and the bbox) for the session
	}
	d.fireLock(sessionID, st, d.frame(sessionID, st, p, f))
}

// frame is OnFrame's work under st.mu; it returns a lock to report once the state is unlocked.
func (d *TrackDetector) frame(sessionID string, st *trackDetectState, p [2]float64, f *telemetryv1.TelemetryFrame) lockEvent {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.user {
		return lockEvent{} // the user's pick wins: detection is bypassed for the session
	}
	st.lastRecv = f.GetRecvTimestamp().AsTime()
	prev, hadPrev := st.last, st.hasLast
	st.last, st.hasLast = p, true
	if st.latched {
		if hadPrev {
			d.watchLock(sessionID, st, prev, p, f)
		}
		return lockEvent{}
	}

	st.frames++
	if hadPrev {
		if step := math.Hypot(p[0]-prev[0], p[1]-prev[1]); step <= maxStepM {
			st.accept(p, step)
		}
	}

	if st.frames >= minCoveragePoints && st.frames%reattemptEveryN == 0 {
		return d.rescore(sessionID, st)
	}
	return lockEvent{}
}

// lockEvent is a lock to report through OnLock (ok false = none).
type lockEvent struct {
	ok    bool
	since time.Time
	slug  string
}

// fireLock calls OnLock for ev. Called WITHOUT st.mu: OnLock does storage I/O (the lap backfill), which
// must not hold up Track() (read by the lap segmenter) or the session's frames.
// A failed backfill is remembered on st for OnSessionEnd's retry (the lock itself stands).
func (d *TrackDetector) fireLock(sessionID string, st *trackDetectState, ev lockEvent) {
	if !ev.ok || d.OnLock == nil {
		return
	}
	// Bounded like the session-end calls: fireLock runs on the ingest goroutine (mid-session locks and
	// the final re-score at session end alike). A timed-out backfill is a failed one - recorded below,
	// retried at session end, and its span kept rather than cleared.
	ctx, cancel := context.WithTimeout(context.Background(), d.endTimeout)
	defer cancel()
	if err := d.OnLock(ctx, sessionID, TimeSpan{From: ev.since}, ev.slug); err != nil {
		st.mu.Lock()
		st.failed = append(st.failed, lockSpan{lockEvent: ev})
		st.mu.Unlock()
	}
}

// watchLock tracks how far the car has driven in a row away from the locked layout and releases the
// lock past releaseAfterM (see releaseDistM). One nearest-point scan of the locked layout per frame
// (a few hundred points), no allocation. A teleport step (> maxStepM) adds no distance: whatever is
// driven after it decides. Caller holds st.mu.
func (d *TrackDetector) watchLock(sessionID string, st *trackDetectState, prev, p [2]float64, f *telemetryv1.TelemetryFrame) {
	step := math.Hypot(p[0]-prev[0], p[1]-prev[1])
	if step > maxStepM {
		return
	}
	if nearestSq(p, st.geoms[st.locked].Points) <= releaseDistM*releaseDistM {
		st.offRunM = 0
		return
	}
	st.offRunM += step
	if st.offRunM < releaseAfterM {
		return
	}
	slug := st.publishedSlug
	start := f.GetRecvTimestamp().AsTime()
	if n := len(st.failed); n > 0 && st.failed[n-1].since.Equal(st.stintStart) {
		st.failed[n-1].until = start // the stint whose backfill failed ends here
	}
	st.locks[len(st.locks)-1].until = start // the latched lock is the last one
	st.newStint(start)
	d.enrich.ClearTrack(sessionID)
	d.log.Info("track lock released: car left the locked track; detecting again",
		zap.String("session_id", sessionID), zap.String("slug", slug))
}

// Track returns the track currently published for sessionID (slug and confidence, "" when none, or
// for an unknown session). The lap segmenter labels each lap with it (LapSegmenter.TrackFn).
func (d *TrackDetector) Track(sessionID string) (slug, confidence string) {
	st := d.session(sessionID)
	if st == nil {
		return "", ""
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.publishedSlug, st.publishedConf
}

// accept adds a vetted position (step metres from the previous one) to the bbox and travelled distance,
// and queues it for scoring once it is sampleStepM from the last sample. Caller holds st.mu.
func (st *trackDetectState) accept(p [2]float64, step float64) {
	if st.hasBbox {
		st.bbox = [4]float64{math.Min(st.bbox[0], p[0]), math.Min(st.bbox[1], p[1]), math.Max(st.bbox[2], p[0]), math.Max(st.bbox[3], p[1])}
	} else {
		st.bbox, st.hasBbox = [4]float64{p[0], p[1], p[0], p[1]}, true
	}
	st.travelled += step
	if !st.hasSample || math.Hypot(p[0]-st.lastSample[0], p[1]-st.lastSample[1]) >= sampleStepM {
		st.pending = append(st.pending, p)
		st.lastSample, st.hasSample = p, true
	}
}

// finite reports whether x is neither NaN nor infinite.
func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// OnSessionEnd makes a final re-score with whatever was driven (bypassing the cadence, but still
// requiring minCoveragePoints), then drops the session's state. A track still only provisional at the
// end was never confirmed (ending early is not evidence for it): it is withdrawn from the published
// track, and OnEnd clears the provisional labels stored on the session's laps.
func (d *TrackDetector) OnSessionEnd(rec *session.Record) {
	// OnEnd runs last, after the final re-score may have locked and backfilled (OnLock), so only labels
	// that no lock ever confirmed are left for it to clear. keep (set below) shields the laps of stints
	// that did lock but whose backfill still fails after a retry. Once per session end, off the frame path.
	var keep []TimeSpan
	if d.OnEnd != nil {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), d.endTimeout)
			defer cancel()
			d.OnEnd(ctx, rec.ID, keep)
		}()
	}
	if d.Override != nil {
		d.Override.end(rec.ID) // picks committed before this are the session's; later ones go elsewhere
	}
	d.mu.Lock()
	st := d.state[rec.ID]
	delete(d.state, rec.ID)
	d.mu.Unlock()
	if st == nil {
		return
	}
	d.syncUser(rec.ID, st, true) // a pick committed since the last frame (or a failed relabel) lands now
	var ev lockEvent
	st.mu.Lock()
	if !st.latched && !st.user && st.frames >= minCoveragePoints {
		ev = d.rescore(rec.ID, st)
	}
	if st.publishedConf == ConfidenceProvisional {
		// Never confirmed: withdraw the guess so the just-ended session's analysis falls back to
		// mini-sectors instead of a possibly wrong layout (a confirmed track stays published).
		st.publishedSlug, st.publishedConf = "", ""
		d.enrich.ClearTrack(rec.ID)
	}
	st.mu.Unlock()
	d.fireLock(rec.ID, st, ev)

	// Retry each failed backfill once; one that still fails keeps its stint's laps (right, just not
	// promoted to confident) out of the clear.
	st.mu.Lock()
	failed := st.failed
	st.mu.Unlock()
	if d.OnLock == nil {
		return
	}
	for _, f := range failed {
		ctx, cancel := context.WithTimeout(context.Background(), d.endTimeout)
		err := d.OnLock(ctx, rec.ID, TimeSpan{From: f.since, Until: f.until}, f.slug)
		cancel()
		if err != nil {
			d.log.Warn("lock backfill still failing at session end; keeping the stint's provisional labels",
				zap.String("session_id", rec.ID), zap.String("slug", f.slug), zap.Error(err))
			keep = append(keep, TimeSpan{From: f.since, Until: f.until})
		}
	}
}

// rescore folds the pending samples into every alive candidate, decides, and publishes on a change. A
// new lock is returned for the caller to report via fireLock once st.mu is released. Caller holds st.mu.
func (d *TrackDetector) rescore(sessionID string, st *trackDetectState) lockEvent {
	st.score()
	v := st.decide()
	if !v.ok {
		// Keep a provisional guess that is still one of the two contenders (avoids flicker while the
		// two are being told apart); withdraw one that nothing supports any more.
		if st.publishedSlug != "" && st.publishedSlug != v.tied[0] && st.publishedSlug != v.tied[1] {
			st.publishedSlug, st.publishedConf = "", ""
			d.enrich.ClearTrack(sessionID)
		}
		return lockEvent{}
	}
	conf := ConfidenceProvisional
	if v.lock {
		conf = ConfidenceConfident
		st.latched = true
		st.locked = v.idx
		st.locks = append(st.locks, lockSpan{lockEvent: lockEvent{ok: true, since: st.stintStart, slug: v.slug}})
	}
	if v.slug == st.publishedSlug && conf == st.publishedConf {
		return lockEvent{}
	}
	st.publishedSlug, st.publishedConf = v.slug, conf
	slug := v.slug
	d.enrich.SetTrack(sessionID, &slug, &conf)
	d.log.Debug("track detection updated",
		zap.String("session_id", sessionID), zap.String("slug", slug), zap.String("confidence", conf),
		zap.Float64("travelled_m", st.travelled))
	return lockEvent{ok: v.lock, since: st.stintStart, slug: slug}
}

// score folds st.pending into every alive candidate: first the bbox pre-filter (the driven bbox only
// grows, so a candidate it rules out is out for good), then each pending point's nearest-neighbour
// distance and apex visit. Cost is O(len(pending) * sum of alive candidates' points); pending holds
// only the samples since the last call, so the whole session's scoring is linear in frames.
func (st *trackDetectState) score() {
	if len(st.pending) == 0 {
		return
	}
	for i := range st.cands {
		c := &st.cands[i]
		if !c.alive {
			continue
		}
		g := &st.geoms[i]
		if g.HasBbox && !bboxContains(st.bbox, g.Bbox, bboxTolerance) {
			c.alive = false // driven path pokes outside this candidate's bbox: cannot be this track
			continue
		}
		trackDir := g.ApexOrderTrusted && len(g.Apexes) >= 3
		var batch float64
		for _, p := range st.pending {
			batch += math.Sqrt(nearestSq(p, g.Points))
			if trackDir {
				c.visitApex(p, g.Apexes)
			}
		}
		c.sum += batch
		c.recent = batch / float64(len(st.pending))
	}
	st.samples += len(st.pending)
	st.pending = st.pending[:0]
}

// visitApex updates the direction tally when p is within apexVisitRadius of an apex other than the
// last one visited: a step to a nearby-later turn (cyclically, within a third of the lap's turns)
// counts as with the layout's direction, a step to a nearby-earlier turn as against it, and a larger
// jump (skipped corners, a teleport) counts as neither.
func (c *candState) visitApex(p [2]float64, apexes [][2]float64) {
	k, best := -1, apexVisitRadius*apexVisitRadius
	for i, a := range apexes {
		if dsq := sq(p[0]-a[0]) + sq(p[1]-a[1]); dsq <= best {
			k, best = i, dsq
		}
	}
	if k < 0 || k == c.lastApex {
		return
	}
	if c.lastApex >= 0 {
		n := len(apexes)
		step := (k - c.lastApex + n) % n
		switch {
		case step <= n/3:
			c.fwd++
		case n-step <= n/3:
			c.back++
		}
	}
	c.lastApex = k
}

// wrongWay reports whether the candidate has been driven against its turn order (see minDirSteps).
func (c *candState) wrongWay() bool { return c.back >= minDirSteps && c.back > c.fwd }

// verdict is one decision: ok reports whether there is a guess worth publishing at all; lock whether
// it should be published as confident and kept for the session.
type verdict struct {
	slug string
	idx  int // geoms index of slug
	ok   bool
	lock bool
	// tied names the two candidates too close to call when !ok because of ambiguity (empty when nothing
	// fits at all), so a provisional guess that is one of them can stay up while the other is not ruled out.
	tied [2]string
}

// decide ranks the alive, right-way candidates by mean nearest-neighbour distance and applies the
// rules:
//   - no guess unless the best fits at all (sanityCap) and beats the runner-up by marginRatio - except
//     an identical-geometry forward/reverse twin pair without trusted apex order (direction cannot be
//     read), which resolves to the forward layout as a last resort; trusted twins wait for direction;
//   - lock once lockCoverageFraction of the best candidate's lap length has been driven, or early once
//     the best is clearly ahead (lockMarginMaxMean / lockMarginRatio after lockMarginMinFraction) and
//     no same-venue sibling layout is still feasible (siblingFeasible); either lock also needs the car
//     to be on the candidate right now (lockRecentMaxM).
//
// Caller holds st.mu.
func (st *trackDetectState) decide() verdict {
	if st.samples == 0 {
		return verdict{}
	}
	best, second := -1, -1
	for i := range st.cands {
		c := &st.cands[i]
		if !c.alive || c.wrongWay() {
			continue
		}
		switch {
		case best < 0 || c.sum < st.cands[best].sum:
			best, second = i, best
		case second < 0 || c.sum < st.cands[second].sum:
			second = i
		}
	}
	if best < 0 {
		return verdict{}
	}
	n := float64(st.samples)
	bestMean := st.cands[best].sum / n
	if bestMean > sanityCap {
		return verdict{} // even the best fit is implausibly bad: nothing fits
	}
	secondMean := math.Inf(1)
	if second >= 0 {
		secondMean = st.cands[second].sum / n
	}

	pick := best
	clear := secondMean > 0 && bestMean/secondMean <= marginRatio
	if !clear {
		if second < 0 {
			return verdict{} // defensive: no runner-up yet no clear win can only mean a non-finite score
		}
		tied := verdict{tied: [2]string{st.geoms[best].Slug, st.geoms[second].Slug}}
		if st.twinOf[best] != second {
			return tied // no clear separation from the runner-up, and not a twin: ambiguous
		}
		if st.geoms[best].ApexOrderTrusted && st.geoms[second].ApexOrderTrusted {
			return tied // direction of travel will rule one twin out (wrongWay); wait for it
		}
		if st.geoms[best].IsReverse {
			pick = second // last resort for a twin whose direction cannot be read: the forward layout
		}
	}

	length := st.geoms[pick].LengthM
	onIt := st.cands[pick].recent <= lockRecentMaxM
	covered := length > 0 && st.travelled >= lockCoverageFraction*length
	margin := clear && bestMean <= lockMarginMaxMean && secondMean >= lockMarginRatio*bestMean &&
		length > 0 && st.travelled >= lockMarginMinFraction*length && !st.siblingFeasible(pick)
	return verdict{slug: st.geoms[pick].Slug, idx: pick, ok: true, lock: onIt && (covered || margin)}
}

// siblingFeasible reports whether another layout of the same venue (same nonzero Base) is still a
// possibility: bbox-feasible and not ruled out by direction. Such a sibling can share most of its
// tarmac with the best candidate (brands-hatch-indy is most of the GP circuit), so the early margin
// lock - which only sees the part driven so far - must not fire while one remains; the full-lap
// coverage lock (with lockRecentMaxM) still can.
func (st *trackDetectState) siblingFeasible(pick int) bool {
	base := st.geoms[pick].Base
	if base == 0 {
		return false
	}
	for i := range st.cands {
		if i != pick && st.geoms[i].Base == base && st.cands[i].alive && !st.cands[i].wrongWay() {
			return true
		}
	}
	return false
}

// isTwin reports whether a and b are the SAME physical track under two directions: opposite IsReverse,
// the same NONZERO Base, and (near-)identical point clouds (order-insensitive - see identicalGeometry).
// The Base guard exists because geometry alone, once compared as an unordered multiset, can no longer
// rule out two genuinely different layouts that happen to share every point; requiring a shared, real
// (nonzero) tracks.base pairing keeps that case ambiguous instead of being wrongly resolved as a twin.
func isTwin(a, b rkb.TrackGeom) bool {
	return a.IsReverse != b.IsReverse && a.Base != 0 && a.Base == b.Base && identicalGeometry(a.Points, b.Points)
}

// identicalGeometry reports whether a and b are the same point cloud (the same set of points, up to
// twinGeometryEpsilon per coordinate) regardless of order: same length, then both clouds are copied and
// sorted by (x, then z) before a pairwise tolerance comparison. Order-insensitive because a reverse
// layout's Points is subsampled-boundary ++ apexes (see rtps/rkb DetectionGeometry) where the boundary
// prefix is copied verbatim from the forward twin but the apex suffix is built by reversing the forward's
// corner order (see tools/rkb/rkb/reverse.py _derive_corners) - so a true twin's two point clouds contain
// the same points, but not in the same stored order, and a per-index comparison would wrongly reject it.
func identicalGeometry(a, b [][2]float64) bool {
	if len(a) != len(b) {
		return false
	}
	sa := append([][2]float64(nil), a...)
	sb := append([][2]float64(nil), b...)
	sortPoints(sa)
	sortPoints(sb)
	for i := range sa {
		if math.Abs(sa[i][0]-sb[i][0]) > twinGeometryEpsilon || math.Abs(sa[i][1]-sb[i][1]) > twinGeometryEpsilon {
			return false
		}
	}
	return true
}

// sortPoints sorts points in place by x, then z: the canonical order identicalGeometry compares two
// point clouds under, so the comparison is insensitive to how each cloud is internally ordered.
func sortPoints(points [][2]float64) {
	sort.Slice(points, func(i, j int) bool {
		if points[i][0] != points[j][0] {
			return points[i][0] < points[j][0]
		}
		return points[i][1] < points[j][1]
	})
}

// bboxContains reports whether driven sits inside cand, widened by tol on every side: containment, not
// similarity, since a partial driven trace is smaller than the full reference track.
func bboxContains(driven, cand [4]float64, tol float64) bool {
	return driven[0] >= cand[0]-tol && driven[1] >= cand[1]-tol &&
		driven[2] <= cand[2]+tol && driven[3] <= cand[3]+tol
}

// nearestSq returns the squared Euclidean distance from p to its nearest point in pts (+Inf if empty).
func nearestSq(p [2]float64, pts [][2]float64) float64 {
	best := math.Inf(1)
	for _, q := range pts {
		if d := sq(p[0]-q[0]) + sq(p[1]-q[1]); d < best {
			best = d
		}
	}
	return best
}

func sq(x float64) float64 { return x * x }

// PointCountForTest returns the number of on-track frames seen for sessionID. Test-only accessor
// (exported so external test packages, needed to avoid an rtps/live<->rtps/processing import cycle,
// can assert accumulation without reaching into unexported state).
func (d *TrackDetector) PointCountForTest(sessionID string) int {
	st := d.session(sessionID)
	if st == nil {
		return 0
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.frames
}
