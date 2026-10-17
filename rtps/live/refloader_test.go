package live

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/processing"
)

// storedRefLap is a 100 m stored reference lap driven at a steady 10 m/s (10 s), 11 samples.
func storedRefLap() *processing.NormalizedLap {
	base := time.Unix(1_600_000_000, 0)
	samples := make([]processing.Sample, 11)
	for i := range samples {
		samples[i] = processing.Sample{Distance: float64(i * 10), RecvTime: base.Add(time.Duration(i) * time.Second)}
	}
	n := processing.Normalize(samples)
	return &n
}

// TestDeltaTracker_StoredReference: with a stored reference loaded, the live gap to it is reported
// once a lap has started at the line (never on the formation lap or a lap joined mid-way); it is
// independent of the session-best delta, and clearing the reference withdraws it.
func TestDeltaTracker_StoredReference(t *testing.T) {
	d := NewDeltaTracker()
	d.SetStoredReference(storedRefLap())
	base := time.Unix(1_700_000_000, 0)

	d.Publish(dframe(0, base, 0, 0)) // formation lap: not from the line
	d.Publish(dframe(20, base.Add(time.Second), 0, 0))
	if _, ok := d.StoredRefDelta(); ok {
		t.Fatal("stored-ref delta on the formation lap")
	}
	start := base.Add(2 * time.Second)
	d.Publish(dframe(0, start, 1, 30*time.Second)) // crosses the line into lap 1
	d.Publish(dframe(40, start.Add(2*time.Second), 1, 0))
	ns, ok := d.StoredRefDelta()
	if !ok || time.Duration(ns) != -2*time.Second { // 40 m in 2 s vs the reference's 4 s
		t.Fatalf("stored-ref delta = %v ok=%v, want -2s", time.Duration(ns), ok)
	}
	if _, ok := d.Delta(); ok {
		t.Error("session-best delta must stay independent (no best lap yet)")
	}

	d.SetStoredReference(nil)
	d.Publish(dframe(50, start.Add(3*time.Second), 1, 0))
	if _, ok := d.StoredRefDelta(); ok {
		t.Error("stored-ref delta after the reference was cleared")
	}

	d.SetStoredReference(storedRefLap())
	d.Publish(dframe(0, start.Add(4*time.Second), 0, 0)) // new session (lap counter back): joined, not from the line
	d.Publish(dframe(10, start.Add(5*time.Second), 0, 0))
	if _, ok := d.StoredRefDelta(); ok {
		t.Error("stored-ref delta right after a session reset")
	}
}

// TestDeltaTracker_PublishAllocFree: the per-frame path with both references loaded allocates nothing
// beyond the amortized lap-sample buffer (it runs on the ingest goroutine at 60 Hz).
func TestDeltaTracker_PublishAllocFree(t *testing.T) {
	d := NewDeltaTracker()
	d.SetStoredReference(storedRefLap())
	base := time.Unix(1_700_000_000, 0)
	buildReferenceLap(d, base)
	frames := make([]*telemetryv1.TelemetryFrame, 4096)
	for i := range frames {
		frames[i] = dframe(float32(i%100), base.Add(10*time.Second+time.Duration(i)*time.Millisecond), 2, 0)
	}
	i := 0
	allocs := testing.AllocsPerRun(2000, func() { d.Publish(frames[i%len(frames)]); i++ })
	if allocs != 0 {
		t.Errorf("Publish allocates %v per frame, want 0", allocs)
	}
	if _, ok := d.StoredRefDelta(); !ok {
		t.Error("no stored-ref delta computed")
	}
}

// fakeTrackEnricher records what the loader forwards.
type fakeTrackEnricher struct {
	mu     sync.Mutex
	sets   int
	clears int
}

func (f *fakeTrackEnricher) SetTrack(string, *string, *string) { f.mu.Lock(); f.sets++; f.mu.Unlock() }
func (f *fakeTrackEnricher) ClearTrack(string)                 { f.mu.Lock(); f.clears++; f.mu.Unlock() }

// hasStored reports whether d currently holds a stored reference.
func hasStored(d *DeltaTracker) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stored != nil
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestRefLoader: the stored reference follows the CONFIDENT track only (loaded off the caller's
// goroutine), is dropped as soon as the track is cleared or provisional, reloads on a pin change for
// the current track only, and a load that finishes after the track moved on is discarded.
func TestRefLoader(t *testing.T) {
	inner := &fakeTrackEnricher{}
	d := NewDeltaTracker()
	var mu sync.Mutex
	loads := map[string]int{}
	gate := make(chan struct{}, 10)
	started := make(chan struct{}, 10) // the worker entered resolve
	l := NewRefLoader(inner, d, func(_ context.Context, slug string) (LoadedRef, error) {
		started <- struct{}{}
		<-gate
		mu.Lock()
		loads[slug]++
		mu.Unlock()
		return LoadedRef{Lap: storedRefLap(), LapTime: time.Minute}, nil
	}, nil)
	defer l.Close()
	count := func(slug string) int { mu.Lock(); defer mu.Unlock(); return loads[slug] }
	slug, prov, conf := "suzuka", processing.ConfidenceProvisional, processing.ConfidenceConfident

	l.SetTrack("s1", &slug, &prov)
	if inner.sets != 1 {
		t.Fatalf("SetTrack not forwarded")
	}
	l.SetTrack("s1", &slug, &conf)
	gate <- struct{}{}
	eventually(t, "load on lock", func() bool { return hasStored(d) })

	l.Reload("other") // not the current track: ignored
	l.Reload("suzuka")
	gate <- struct{}{}
	eventually(t, "reload on pin change", func() bool { return count("suzuka") == 2 })

	l.ClearTrack("s1")
	if hasStored(d) || inner.clears != 1 {
		t.Fatalf("clear: stored=%v clears=%d", hasStored(d), inner.clears)
	}

	// A load in flight when the track changes must not install a stale reference.
	for len(started) > 0 { // drop the earlier loads' signals
		<-started
	}
	l.SetTrack("s1", &slug, &conf)
	<-started                      // the load is in flight ...
	l.SetTrack("s1", &slug, &prov) // ... when the track goes back to provisional
	gate <- struct{}{}
	eventually(t, "stale load to finish", func() bool { return count("suzuka") == 3 })
	time.Sleep(10 * time.Millisecond)
	if hasStored(d) {
		t.Error("stale load installed after the track went provisional")
	}
}

// fakeRefs is a scripted stored-reference source for the LapStored tests: what resolve returns now.
type fakeRefs struct {
	mu    sync.Mutex
	ref   LoadedRef
	loads int
}

func (f *fakeRefs) resolve(context.Context, string) (LoadedRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	return f.ref, nil
}

func (f *fakeRefs) set(r LoadedRef) { f.mu.Lock(); f.ref = r; f.mu.Unlock() }
func (f *fakeRefs) count() int      { f.mu.Lock(); defer f.mu.Unlock(); return f.loads }

// eligibleLap is a stored lap that can be slug's reference.
func eligibleLap(slug string, dur time.Duration) processing.Lap {
	return processing.Lap{SessionID: "s1", LapNumber: 1, Duration: dur, Timed: true, Complete: true, Valid: true,
		TrackSlug: slug, TrackConfidence: processing.ConfidenceConfident}
}

// lockedLoader returns a loader locked on suzuka whose initial load has finished.
func lockedLoader(t *testing.T, refs *fakeRefs, d *DeltaTracker) *RefLoader {
	t.Helper()
	l := NewRefLoader(&fakeTrackEnricher{}, d, refs.resolve, nil)
	slug, conf := "suzuka", processing.ConfidenceConfident
	l.SetTrack("s1", &slug, &conf)
	eventually(t, "initial load", func() bool { return refs.count() == 1 })
	time.Sleep(10 * time.Millisecond) // let the worker install the result
	return l
}

// TestRefLoader_FirstStoredLapBecomesReference: on a track with nothing stored the lock finds no
// reference; the first eligible lap stored on it is loaded, and the stored-ref delta appears on the
// next lap. Laps on another track, or not eligible, change nothing.
func TestRefLoader_FirstStoredLapBecomesReference(t *testing.T) {
	refs := &fakeRefs{}
	d := NewDeltaTracker()
	l := lockedLoader(t, refs, d)
	defer l.Close()
	if hasStored(d) {
		t.Fatal("a reference was installed for a track with nothing stored")
	}

	l.LapStored(eligibleLap("red-bull-ring", 90*time.Second)) // another track
	notEligible := eligibleLap("suzuka", 90*time.Second)
	notEligible.Valid = false
	l.LapStored(notEligible)
	time.Sleep(20 * time.Millisecond)
	if refs.count() != 1 {
		t.Fatalf("loads = %d, want no reload for another track / an ineligible lap", refs.count())
	}

	refs.set(LoadedRef{Lap: storedRefLap(), LapTime: 100 * time.Second})
	l.LapStored(eligibleLap("suzuka", 100*time.Second))
	eventually(t, "first stored lap loaded", func() bool { return hasStored(d) })

	base := time.Unix(1_700_000_000, 0)
	d.Publish(dframe(90, base.Add(-time.Second), 1, 0)) // end of the lap just stored ...
	d.Publish(dframe(0, base, 2, 100*time.Second))      // ... the line into the next lap
	d.Publish(dframe(40, base.Add(2*time.Second), 2, 0))
	if _, ok := d.StoredRefDelta(); !ok {
		t.Error("delta_to_stored_ref still null on the lap after the first stored lap")
	}
}

// TestRefLoader_FasterLapReplacesUnpinnedReference: a new fastest stored lap reloads an unpinned
// (fastest) reference; a slower one does not.
func TestRefLoader_FasterLapReplacesUnpinnedReference(t *testing.T) {
	refs := &fakeRefs{ref: LoadedRef{Lap: storedRefLap(), LapTime: 100 * time.Second}}
	l := lockedLoader(t, refs, NewDeltaTracker())
	defer l.Close()

	l.LapStored(eligibleLap("suzuka", 101*time.Second))
	time.Sleep(20 * time.Millisecond)
	if refs.count() != 1 {
		t.Fatalf("loads = %d after a slower lap, want 1", refs.count())
	}
	refs.set(LoadedRef{Lap: storedRefLap(), LapTime: 98 * time.Second})
	l.LapStored(eligibleLap("suzuka", 98*time.Second))
	eventually(t, "reload on a new fastest lap", func() bool { return refs.count() == 2 })
	time.Sleep(10 * time.Millisecond)
	l.LapStored(eligibleLap("suzuka", 99*time.Second)) // slower than the newly loaded 98 s
	time.Sleep(20 * time.Millisecond)
	if refs.count() != 2 {
		t.Errorf("loads = %d, want the 98 s lap to be the new bar", refs.count())
	}
}

// TestRefLoader_PinnedReferenceKept: a pinned reference is never replaced by a faster stored lap.
func TestRefLoader_PinnedReferenceKept(t *testing.T) {
	refs := &fakeRefs{ref: LoadedRef{Lap: storedRefLap(), LapTime: 100 * time.Second, Pinned: true}}
	l := lockedLoader(t, refs, NewDeltaTracker())
	defer l.Close()
	l.LapStored(eligibleLap("suzuka", 80*time.Second))
	time.Sleep(20 * time.Millisecond)
	if refs.count() != 1 {
		t.Errorf("loads = %d, want the pinned reference kept", refs.count())
	}
}

// TestRefLoader_ReloadsWhenItsSessionIsRemoved: deleting or discarding the session that holds the
// loaded reference (its pin cascades away) reloads, so the delta falls back to the fastest lap; any
// other session's removal changes nothing.
func TestRefLoader_ReloadsWhenItsSessionIsRemoved(t *testing.T) {
	refs := &fakeRefs{ref: LoadedRef{Lap: storedRefLap(), LapTime: 100 * time.Second, Pinned: true, SessionID: "s9"}}
	l := lockedLoader(t, refs, NewDeltaTracker())
	defer l.Close()
	l.SessionRemoved("other")
	time.Sleep(20 * time.Millisecond)
	if refs.count() != 1 {
		t.Fatalf("loads = %d after an unrelated removal, want 1", refs.count())
	}
	refs.set(LoadedRef{Lap: storedRefLap(), LapTime: 101 * time.Second, SessionID: "s1"})
	l.SessionRemoved("s9")
	eventually(t, "reload after the reference's session was removed", func() bool { return refs.count() == 2 })
}

// failingRefs fails the first n resolves, then succeeds.
type failingRefs struct {
	fakeRefs
	fails int
}

func (f *failingRefs) resolve(ctx context.Context, slug string) (LoadedRef, error) {
	ref, _ := f.fakeRefs.resolve(ctx, slug)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loads <= f.fails {
		return LoadedRef{}, errors.New("db down")
	}
	return ref, nil
}

// TestRefLoader_RetriesFailedLoad: a failed load is retried with backoff until it succeeds, and only
// while the request is current: a track change stops the retries.
func TestRefLoader_RetriesFailedLoad(t *testing.T) {
	refs := &failingRefs{fakeRefs: fakeRefs{ref: LoadedRef{Lap: storedRefLap(), LapTime: time.Minute}}, fails: 2}
	d := NewDeltaTracker()
	l := NewRefLoader(&fakeTrackEnricher{}, d, refs.resolve, nil)
	l.retryBase = time.Millisecond
	defer l.Close()
	slug, conf := "suzuka", processing.ConfidenceConfident
	l.SetTrack("s1", &slug, &conf)
	eventually(t, "load after two failures", func() bool { return hasStored(d) })
	if refs.count() != 3 {
		t.Errorf("loads = %d, want 3 (2 failures + 1 success)", refs.count())
	}

	always := &failingRefs{fails: 1 << 30}
	l2 := NewRefLoader(&fakeTrackEnricher{}, NewDeltaTracker(), always.resolve, nil)
	l2.retryBase = time.Millisecond
	defer l2.Close()
	l2.SetTrack("s1", &slug, &conf)
	eventually(t, "a few retries", func() bool { return always.count() >= 3 })
	l2.ClearTrack("s1")
	time.Sleep(20 * time.Millisecond)
	n := always.count()
	time.Sleep(50 * time.Millisecond)
	if always.count() != n {
		t.Errorf("retries continued after the track was cleared: %d -> %d", n, always.count())
	}
}
