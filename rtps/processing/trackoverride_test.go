package processing_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// overrideRig wires a TrackDetector with a user TrackOverride the way the backend does: the segmenter
// labels laps from detector.Track, OnLock backfills, OnUserTrack relabels the session, OnEnd clears
// provisional labels, and a real RefLoader sits between the detector and the enrichment state (resolve
// records every slug it is asked to load).
type overrideRig struct {
	t      *testing.T
	d      *processing.TrackDetector
	o      *processing.TrackOverride
	seg    *processing.LapSegmenter
	store  *processing.MemLapStore
	enrich *live.EnrichmentState
	loader *live.RefLoader

	mu       sync.Mutex
	resolved []string

	recs map[string]*session.Record
	n    int
}

func newOverrideRig(t *testing.T) *overrideRig {
	t.Helper()
	r := &overrideRig{t: t, store: processing.NewMemLapStore(),
		enrich: live.NewEnrichmentState(), recs: map[string]*session.Record{}}
	r.loader = live.NewRefLoader(r.enrich, live.NewDeltaTracker(), func(_ context.Context, slug string) (live.LoadedRef, error) {
		r.mu.Lock()
		r.resolved = append(r.resolved, slug)
		r.mu.Unlock()
		return live.LoadedRef{}, nil
	}, nil)
	t.Cleanup(r.loader.Close)
	geoms := []rkb.TrackGeom{{Slug: "track-a", Points: circlePoints(0, 0, loopRadius, loopN), LengthM: loopLength}, farTrack()}
	r.d = processing.NewTrackDetector(&fakeGeomProvider{geoms: geoms}, r.loader, nil)
	r.o = processing.NewTrackOverride(r.d)
	r.d.OnLock = func(ctx context.Context, id string, span processing.TimeSpan, slug string) error {
		return r.store.LabelTrack(ctx, id, span, slug)
	}
	r.d.OnUserTrack = func(ctx context.Context, id, slug, confidence string) error {
		if err := r.store.SetSessionTrack(ctx, id, slug, confidence); err != nil {
			return err
		}
		r.loader.Reload(slug)
		return nil
	}
	r.d.OnEnd = func(ctx context.Context, id string, keep []processing.TimeSpan) {
		_ = r.store.ClearProvisionalLabels(ctx, id, keep...)
	}
	r.seg = processing.NewLapSegmenter(r.store)
	r.seg.TrackFn = r.d.Track
	return r
}

func (r *overrideRig) start(id string) {
	r.recs[id] = &session.Record{ID: id}
	r.seg.OnSessionStart(r.recs[id])
	r.d.OnSessionStart(r.recs[id])
}

func (r *overrideRig) end(id string) {
	r.seg.OnSessionEnd(r.recs[id])
	r.d.OnSessionEnd(r.recs[id])
}

// drive feeds pts as on-track frames on lap, segmenter first (as the sink fan-out orders them).
func (r *overrideRig) drive(id string, lap int32, pts [][2]float64) {
	base := time.Unix(1_700_000_000, 0)
	for _, p := range pts {
		f := posFrame(true, float32(p[0]), float32(p[1]), lap, uint32(r.n))
		f.RecvTimestamp = timestamppb.New(base.Add(time.Duration(r.n) * time.Second / 60))
		f.LastLap = durationpb.New(time.Minute)
		r.n++
		r.seg.OnFrame(id, f)
		r.d.OnFrame(id, f)
	}
}

// track returns the published live track as "slug/confidence" ("" when none).
func (r *overrideRig) track() string {
	en := r.enrich.Get()
	if en.Track == nil {
		return ""
	}
	return *en.Track + "/" + *en.TrackConfidence
}

func (r *overrideRig) labels(id string) map[int32]string {
	out := map[int32]string{}
	for _, l := range r.store.BySession(id) {
		out[l.LapNumber] = l.TrackSlug + "/" + l.TrackConfidence
	}
	return out
}

func (r *overrideRig) assertLabels(id, want string) {
	r.t.Helper()
	got := r.labels(id)
	if len(got) == 0 {
		r.t.Fatalf("%s: no laps stored", id)
	}
	for n, l := range got {
		if l != want {
			r.t.Errorf("%s lap %d = %s, want %s (all: %v)", id, n, l, want, got)
		}
	}
}

// waitResolved waits for the RefLoader worker to have been asked for slug.
func (r *overrideRig) waitResolved(slug string) {
	r.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, s := range r.resolved {
			if s == slug {
				r.mu.Unlock()
				return
			}
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatalf("reference loader never resolved %q (got %v)", slug, r.resolved)
}

var overrideLoop = jitterPoints(circlePoints(0, 0, loopRadius, loopN))

// TestTrackOverride_PickDuringSession: a pick while a session is open applies to it at the next frame:
// published at once as user/confident, the laps already stored (here labelled by a detector lock on
// another track) are relabelled, later laps carry it, the stored reference is loaded, and the
// detector's own disagreeing match never changes the published track, not even a release-worthy
// excursion. The labels survive the session end.
func TestTrackOverride_PickDuringSession(t *testing.T) {
	r := newOverrideRig(t)
	r.start("s1")
	r.drive("s1", 0, overrideLoop)
	r.drive("s1", 1, overrideLoop[:10])
	if got := r.labels("s1")[0]; got != "track-a/confident" {
		t.Fatalf("setup: lap 0 = %s, want the detector's track-a/confident", got)
	}

	applies, id := r.o.Set("suzuka")
	if applies != "current" || id != "s1" {
		t.Fatalf("Set = %q, %q; want current, s1", applies, id)
	}
	r.drive("s1", 1, overrideLoop[10:11])
	if got := r.track(); got != "suzuka/confident" {
		t.Fatalf("published = %q, want suzuka/confident", got)
	}
	if got := r.o.UserTrack(); got != "suzuka" {
		t.Fatalf("UserTrack = %q, want suzuka", got)
	}
	if got := r.labels("s1")[0]; got != "suzuka/confident" {
		t.Fatalf("lap 0 after pick = %s, want suzuka/confident (backfilled over the detector's lock)", got)
	}
	r.waitResolved("suzuka")

	// The detector keeps seeing track-a, then a long excursion that would release a lock: ignored.
	r.drive("s1", 1, overrideLoop[11:])
	r.drive("s1", 2, overrideLoop)
	r.drive("s1", 3, circlePoints(100000, 100000, 2000, 400))
	if got := r.track(); got != "suzuka/confident" {
		t.Fatalf("published after disagreeing detection = %q, want suzuka/confident", got)
	}
	r.end("s1")
	r.assertLabels("s1", "suzuka/confident")
	if got := r.track(); got != "suzuka/confident" {
		t.Fatalf("published after end = %q, want suzuka/confident retained", got)
	}
}

// TestTrackOverride_PendingThenNextSession: a pick with no session open waits, then applies the
// moment the next session starts (before any frame), and labels all its laps.
func TestTrackOverride_PendingThenNextSession(t *testing.T) {
	r := newOverrideRig(t)
	applies, id := r.o.Set("suzuka")
	if applies != "next" || id != "" {
		t.Fatalf("Set = %q, %q; want next, \"\"", applies, id)
	}
	if got := r.o.Pending(); got != "suzuka" {
		t.Fatalf("Pending = %q, want suzuka", got)
	}
	r.start("s1")
	if got := r.track(); got != "suzuka/confident" {
		t.Fatalf("published at start = %q, want suzuka/confident", got)
	}
	if got := r.o.Pending(); got != "" {
		t.Fatalf("Pending after start = %q, want none", got)
	}
	r.waitResolved("suzuka")
	r.drive("s1", 0, overrideLoop)
	r.drive("s1", 1, overrideLoop)
	r.drive("s1", 2, overrideLoop[:5])
	r.end("s1")
	r.assertLabels("s1", "suzuka/confident")
}

// TestTrackOverride_ClearedAtSessionEnd: the pick belongs to one session; the next one detects again.
func TestTrackOverride_ClearedAtSessionEnd(t *testing.T) {
	r := newOverrideRig(t)
	r.o.Set("suzuka")
	r.start("s1")
	r.drive("s1", 0, overrideLoop[:50])
	r.end("s1")

	r.start("s2")
	if got := r.track(); got != "" {
		t.Fatalf("published at the next session's start = %q, want none", got)
	}
	if got := r.o.UserTrack(); got != "" {
		t.Fatalf("UserTrack in the next session = %q, want none", got)
	}
	r.drive("s2", 0, overrideLoop)
	r.drive("s2", 1, overrideLoop[:5])
	if got := r.track(); got != "track-a/confident" {
		t.Fatalf("next session published %q, want detection's track-a/confident", got)
	}
	r.assertLabels("s2", "track-a/confident")
}

// TestTrackOverride_DeleteDuringSession: clearing an applied pick withdraws it (track cleared, labels
// downgraded to provisional) and detection resumes for the whole session: its lock relabels them.
func TestTrackOverride_DeleteDuringSession(t *testing.T) {
	r := newOverrideRig(t)
	r.start("s1")
	r.o.Set("suzuka")
	r.drive("s1", 0, overrideLoop[:300])
	r.drive("s1", 1, overrideLoop[:2])
	if got := r.labels("s1")[0]; got != "suzuka/confident" {
		t.Fatalf("setup: lap 0 = %s, want suzuka/confident", got)
	}

	r.o.Clear()
	r.drive("s1", 1, overrideLoop[2:3])
	if got := r.track(); got != "" {
		t.Fatalf("published after delete = %q, want none (detection restarts)", got)
	}
	if got := r.o.UserTrack(); got != "" {
		t.Fatalf("UserTrack after delete = %q, want none", got)
	}
	if got := r.labels("s1")[0]; got != "suzuka/provisional" {
		t.Fatalf("lap 0 after delete = %s, want suzuka/provisional", got)
	}
	r.drive("s1", 1, overrideLoop[3:])
	r.drive("s1", 2, overrideLoop[:5])
	r.end("s1")
	r.assertLabels("s1", "track-a/confident")
}

// TestTrackOverride_DeletePending: clearing a pending pick means nothing is applied.
func TestTrackOverride_DeletePending(t *testing.T) {
	r := newOverrideRig(t)
	r.o.Set("suzuka")
	r.o.Clear()
	if got := r.o.Pending(); got != "" {
		t.Fatalf("Pending after delete = %q, want none", got)
	}
	r.start("s1")
	r.drive("s1", 0, overrideLoop[:20])
	if got := r.track(); got == "suzuka/confident" {
		t.Fatal("a deleted pending pick was applied")
	}
}

// TestTrackOverride_PickReplaced: picking again replaces the pick, relabelling the session.
func TestTrackOverride_PickReplaced(t *testing.T) {
	r := newOverrideRig(t)
	r.start("s1")
	r.o.Set("suzuka")
	r.drive("s1", 0, overrideLoop)
	r.drive("s1", 1, overrideLoop[:2])
	r.o.Set("monza")
	r.drive("s1", 1, overrideLoop[2:4])
	if got := r.track(); got != "monza/confident" {
		t.Fatalf("published = %q, want monza/confident", got)
	}
	r.end("s1")
	r.assertLabels("s1", "monza/confident")
}

// TestTrackOverride_PickRacesSessionEnd: "current" means committed. A pick made while the session is
// open is the session's even when it ends before another frame arrives: its laps keep the pick. A pick
// made after the end waits for the next session and never touches the ended one.
func TestTrackOverride_PickRacesSessionEnd(t *testing.T) {
	r := newOverrideRig(t)
	r.start("s1")
	r.drive("s1", 0, overrideLoop[:200])
	r.drive("s1", 1, overrideLoop[:5])
	if applies, id := r.o.Set("suzuka"); applies != "current" || id != "s1" {
		t.Fatalf("Set = %q, %q; want current, s1", applies, id)
	}
	if slug, conf := r.d.Track("s1"); slug != "suzuka" || conf != processing.ConfidenceConfident {
		t.Fatalf("Track right after Set = %s/%s, want suzuka/confident (committed, not queued)", slug, conf)
	}
	r.end("s1") // no frame between the pick and the end
	r.assertLabels("s1", "suzuka/confident")

	r.start("s2")
	r.drive("s2", 0, overrideLoop[:20])
	if got := r.track(); got == "suzuka/confident" {
		t.Fatal("a pick for s1 was applied to s2")
	}
	if got := r.o.Pending(); got != "" {
		t.Fatalf("Pending = %q, want none", got)
	}

	r.end("s2")
	if applies, id := r.o.Set("monza"); applies != "next" || id != "" {
		t.Fatalf("Set after end = %q, %q; want next, \"\"", applies, id)
	}
	r.assertLabels("s1", "suzuka/confident")
	r.start("s3")
	if got := r.track(); got != "monza/confident" {
		t.Fatalf("s3 published %q, want monza/confident", got)
	}
}

// TestTrackOverride_DeleteRestoresPrePickLocks: withdrawing a pick gives back the labels detection had
// confirmed before it (laps started inside a pre-pick lock), so an unconfirmed session end clears
// only the laps the pick had labelled, never the ones detection had already locked.
func TestTrackOverride_DeleteRestoresPrePickLocks(t *testing.T) {
	r := newOverrideRig(t)
	r.start("s1")
	r.drive("s1", 0, overrideLoop)
	r.drive("s1", 1, overrideLoop[:10])
	if got := r.labels("s1")[0]; got != "track-a/confident" {
		t.Fatalf("setup: lap 0 = %s, want the detector's track-a/confident", got)
	}
	r.o.Set("suzuka")
	r.drive("s1", 1, overrideLoop[10:12])
	r.drive("s1", 2, overrideLoop[:3]) // lap 1 (started under the lock) saved
	r.drive("s1", 3, overrideLoop[:3]) // lap 2 (started after the pick) saved
	want := map[int32]string{0: "suzuka/confident", 1: "suzuka/confident", 2: "suzuka/confident"}
	if got := r.labels("s1"); !sameLabels(got, want) {
		t.Fatalf("after pick: %v, want %v", got, want)
	}

	r.o.Clear()
	r.drive("s1", 3, overrideLoop[3:5]) // too little for detection to lock again
	r.end("s1")
	want = map[int32]string{0: "track-a/confident", 1: "track-a/confident", 2: "/", 3: "/"}
	if got := r.labels("s1"); !sameLabels(got, want) {
		t.Fatalf("after delete + end: %v, want %v", got, want)
	}
}

func sameLabels(got, want map[int32]string) bool {
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// TestTrackOverride_MultiStream: with two sessions open (two gRPC streams), "current" is the most
// recently started one still open; another session starting never drops an applied pick, and when the
// newer session ends the older one becomes current again (not "next").
func TestTrackOverride_MultiStream(t *testing.T) {
	r := newOverrideRig(t)
	r.start("s1")
	if applies, id := r.o.Set("suzuka"); applies != "current" || id != "s1" {
		t.Fatalf("Set = %q, %q; want current, s1", applies, id)
	}
	r.start("s2")
	if slug, _ := r.d.Track("s1"); slug != "suzuka" {
		t.Fatalf("s1 track after s2 started = %q, want suzuka", slug)
	}
	if applies, id := r.o.Set("monza"); applies != "current" || id != "s2" {
		t.Fatalf("Set with s1+s2 open = %q, %q; want current, s2", applies, id)
	}
	r.end("s2")
	if applies, id := r.o.Set("spa"); applies != "current" || id != "s1" {
		t.Fatalf("Set after s2 ended = %q, %q; want current, s1", applies, id)
	}
	if slug, _ := r.d.Track("s1"); slug != "spa" {
		t.Fatalf("s1 track = %q, want spa", slug)
	}
	r.end("s1")
	if applies, _ := r.o.Set("monza"); applies != "next" {
		t.Fatalf("Set with nothing open = %q, want next", applies)
	}
}

// TestTrackOverride_NoStaleTrackAtSessionStart: the enrich sink's session-start reset keeps the
// track (the detector's) but must not carry the previous session's track into a new session with no
// pending pick, whichever of the two sinks runs first; a pending pick shows in either order.
func TestTrackOverride_NoStaleTrackAtSessionStart(t *testing.T) {
	for _, detectorFirst := range []bool{true, false} {
		for _, pick := range []string{"", "suzuka"} {
			r := newOverrideRig(t)
			es := live.NewEnrichSink(analysis.Analyzer{}, r.enrich, nil)
			r.start("s1")
			r.drive("s1", 0, overrideLoop)
			r.drive("s1", 1, overrideLoop[:5])
			r.end("s1")
			if got := r.track(); got != "track-a/confident" {
				t.Fatalf("setup: published %q, want track-a/confident retained after end", got)
			}
			if pick != "" {
				r.o.Set(pick)
			}
			rec := &session.Record{ID: "s2"}
			if detectorFirst {
				r.d.OnSessionStart(rec)
				es.OnSessionStart(rec)
			} else {
				es.OnSessionStart(rec)
				r.d.OnSessionStart(rec)
			}
			want := ""
			if pick != "" {
				want = pick + "/confident"
			}
			if got := r.track(); got != want {
				t.Errorf("detectorFirst=%v pick=%q: published %q at s2 start, want %q", detectorFirst, pick, got, want)
			}
			es.Close()
		}
	}
}

// TestTrackOverride_ConcurrentPicks: picks, clears and reads from other goroutines while the ingest
// side cycles sessions (run under -race).
func TestTrackOverride_ConcurrentPicks(t *testing.T) {
	r := newOverrideRig(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%3 == 0 {
				r.o.Clear()
			} else {
				r.o.Set("suzuka")
			}
			_, _ = r.o.Pending(), r.o.UserTrack()
		}
	}()
	for i := 0; i < 20; i++ {
		id := "s" + string(rune('a'+i))
		r.start(id)
		r.drive(id, 0, overrideLoop[:30])
		r.end(id)
	}
	close(stop)
	wg.Wait()
}
