package live

import (
	"sync"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
	"github.com/arush-sal/garage/backend/rtps/session"
)

func testAnalyzer() analysis.Analyzer {
	return analysis.Analyzer{
		Laps: processing.NewMemLapStore(), Traces: processing.NewMemTraceStore(),
		KB: reference.NewStaticKB(), FallbackSectors: 3,
	}
}

// recordingLapStore wraps a LapStore and counts BySession calls per session id, so a test can prove
// AnalyzeSession actually ran for a given session (rather than being evicted by a concurrent request
// for a different session) without depending on EnrichmentState's single-slot last-write-wins storage.
type recordingLapStore struct {
	inner processing.LapStore
	mu    sync.Mutex
	calls map[string]int
}

func newRecordingLapStore(inner processing.LapStore) *recordingLapStore {
	return &recordingLapStore{inner: inner, calls: map[string]int{}}
}

func (r *recordingLapStore) Save(lap *processing.Lap) error { return r.inner.Save(lap) }

func (r *recordingLapStore) BySession(sessionID string) []*processing.Lap {
	r.mu.Lock()
	r.calls[sessionID]++
	r.mu.Unlock()
	return r.inner.BySession(sessionID)
}

func (r *recordingLapStore) List() []*processing.Lap { return r.inner.List() }
func (r *recordingLapStore) SessionByFingerprint(fp string) (string, bool, error) {
	return r.inner.SessionByFingerprint(fp)
}
func (r *recordingLapStore) DeleteSession(id string) error { return r.inner.DeleteSession(id) }

func (r *recordingLapStore) callCount(sessionID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[sessionID]
}

// TestEnrichSink_ConcurrentSessionsNoEviction proves two sessions recomputing concurrently each get
// their own enrichment set (staff review defect #3: a single global request channel let a second
// session's request evict the first's). Run with -race.
func TestEnrichSink_ConcurrentSessionsNoEviction(t *testing.T) {
	laps := newRecordingLapStore(processing.NewMemLapStore())
	analyzer := analysis.Analyzer{
		Laps: laps, Traces: processing.NewMemTraceStore(), KB: reference.NewStaticKB(), FallbackSectors: 3,
	}
	state := NewEnrichmentState()
	e := NewEnrichSink(analyzer, state, func(err error) { t.Errorf("unexpected onErr: %v", err) })
	defer e.Close()

	recA := &session.Record{ID: "session-a", CurrentLap: 0}
	recB := &session.Record{ID: "session-b", CurrentLap: 0}
	e.OnSessionStart(recA)
	e.OnSessionStart(recB)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		e.OnFrame("session-a", &telemetryv1.TelemetryFrame{CurrentLap: 1})
	}()
	go func() {
		defer wg.Done()
		e.OnFrame("session-b", &telemetryv1.TelemetryFrame{CurrentLap: 1})
	}()
	wg.Wait()

	// Both sessions' requests must eventually reach AnalyzeSession — neither may be evicted by the
	// other's concurrent request (staff review defect #3, single global request channel). We assert
	// via the underlying LapStore's per-session call count rather than EnrichmentState, since that
	// state is a single last-write-wins slot and cannot hold two sessions' results simultaneously.
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for laps.callCount("session-a") == 0 || laps.callCount("session-b") == 0 {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatalf("expected AnalyzeSession for both sessions, got calls a=%d b=%d",
				laps.callCount("session-a"), laps.callCount("session-b"))
		}
	}

	// End-to-end sanity: the state eventually reflects one of the two sessions' published analysis.
	deadline = time.After(time.Second)
	for {
		if en := state.Get(); en.Analysis != nil && (en.SessionID == "session-a" || en.SessionID == "session-b") {
			break
		}
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("EnrichmentState never published an analysis for either session")
		}
	}
}

// blockingLapStore wraps a LapStore whose BySession call blocks on a channel until released, so a test
// can put an AnalyzeSession call in flight and hold it there deterministically. AnalyzeSession calls
// Laps.BySession first, so blocking there is sufficient without touching TraceStore.
type blockingLapStore struct {
	inner   processing.LapStore
	release chan struct{}
	entered chan struct{} // closed once BySession has been entered, so the test can observe "in flight"
	once    sync.Once
}

func newBlockingLapStore(inner processing.LapStore) *blockingLapStore {
	return &blockingLapStore{inner: inner, release: make(chan struct{}), entered: make(chan struct{})}
}

func (b *blockingLapStore) Save(lap *processing.Lap) error { return b.inner.Save(lap) }

func (b *blockingLapStore) BySession(sessionID string) []*processing.Lap {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.inner.BySession(sessionID)
}

func (b *blockingLapStore) List() []*processing.Lap { return b.inner.List() }
func (b *blockingLapStore) SessionByFingerprint(fp string) (string, bool, error) {
	return b.inner.SessionByFingerprint(fp)
}
func (b *blockingLapStore) DeleteSession(id string) error { return b.inner.DeleteSession(id) }

// TestEnrichSink_NoStaleRepublishAfterEnd is the regression for the staff-review-flagged race: the
// async worker can still be inside AnalyzeSession for a session when OnSessionEnd fires. Without the
// active-session fence, drain's e.state.Set after AnalyzeSession returns would republish the ended
// session's analysis over the clear that OnSessionEnd already published. Run with -race.
func TestEnrichSink_NoStaleRepublishAfterEnd(t *testing.T) {
	laps := newBlockingLapStore(processing.NewMemLapStore())
	analyzer := analysis.Analyzer{
		Laps: laps, Traces: processing.NewMemTraceStore(), KB: reference.NewStaticKB(), FallbackSectors: 3,
	}
	state := NewEnrichmentState()
	e := NewEnrichSink(analyzer, state, func(err error) { t.Errorf("unexpected onErr: %v", err) })
	defer e.Close()

	rec := &session.Record{ID: "session-stale", CurrentLap: 0}
	e.OnSessionStart(rec)
	e.OnFrame("session-stale", &telemetryv1.TelemetryFrame{CurrentLap: 1}) // enqueues + wakes worker

	// Wait for the worker to actually be inside AnalyzeSession (blocked in BySession) before ending the
	// session, so the end genuinely races the in-flight recompute rather than preceding it.
	select {
	case <-laps.entered:
	case <-time.After(time.Second):
		t.Fatal("worker never entered AnalyzeSession (BySession) in time")
	}

	e.OnSessionEnd(rec) // clears + fences while the recompute is still blocked mid-flight
	close(laps.release) // let the blocked AnalyzeSession proceed to completion

	// The enrichment must be, and must STAY, cleared: the fenced recompute must never republish
	// session-stale's analysis over the clear. Poll for a sustained window rather than a single check.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if en := state.Get(); en.Analysis != nil {
			t.Fatalf("stale recompute republished after OnSessionEnd: %+v", en)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestEnrichSink_NoStaleRepublishAcrossCollidingGenerations is the regression for the staff-review-
// flagged generation-collision race: session IDs (e.g. "sess-N") are minted per-Detector, so they are
// only unique within a single gRPC stream and can collide across streams (reconnect, or concurrent
// collectors sharing one EnrichSink). Without a generation-keyed fence, a stale in-flight recompute for
// an ended generation-1 session could republish over a newer, colliding-ID generation-2 session's state,
// even though drain's plain id-presence re-check (active[id] exists) would incorrectly pass. Run with -race.
func TestEnrichSink_NoStaleRepublishAcrossCollidingGenerations(t *testing.T) {
	laps := newBlockingLapStore(processing.NewMemLapStore())
	analyzer := analysis.Analyzer{
		Laps: laps, Traces: processing.NewMemTraceStore(), KB: reference.NewStaticKB(), FallbackSectors: 3,
	}
	state := NewEnrichmentState()
	e := NewEnrichSink(analyzer, state, func(err error) { t.Errorf("unexpected onErr: %v", err) })
	defer e.Close()

	const id = "sess-1"
	rec1 := &session.Record{ID: id, CurrentLap: 0}
	e.OnSessionStart(rec1)                                    // generation 1
	e.OnFrame(id, &telemetryv1.TelemetryFrame{CurrentLap: 1}) // enqueues + wakes worker (gen 1)

	// Wait for the worker to actually be inside AnalyzeSession (blocked in BySession) for generation 1,
	// so the id reuse below genuinely races the in-flight gen-1 recompute rather than preceding it.
	select {
	case <-laps.entered:
	case <-time.After(time.Second):
		t.Fatal("worker never entered AnalyzeSession (BySession) in time")
	}

	e.OnSessionEnd(rec1) // ends gen 1: clears state, drops gen 1 from active

	// A new, colliding-ID session starts: same id "sess-1", but a fresh generation. Because no lap
	// boundary fires for it, no recompute is requested for generation 2 — its expected published state
	// is exactly OnSessionStart's reset (SessionID set, Analysis nil).
	rec2 := &session.Record{ID: id, CurrentLap: 0}
	e.OnSessionStart(rec2) // generation 2

	close(laps.release) // let the blocked generation-1 AnalyzeSession proceed to completion

	// The state must reflect generation 2's reset (Analysis nil), and must NEVER show generation 1's
	// stale non-nil analysis republished over it. Poll for a sustained window rather than a single check.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		en := state.Get()
		if en.Analysis != nil {
			t.Fatalf("stale generation-1 recompute republished over generation-2's state: %+v", en)
		}
		if en.SessionID != id {
			t.Fatalf("unexpected session id in state: %+v", en)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestOnSessionEnd_RetainsEnrichment is the regression for the race-lifecycle A1 requirement: the
// finished race's enrichment (session_id in particular) must survive OnSessionEnd so the live view
// still carries post-race context until the next OnSessionStart overwrites it. Previously OnSessionEnd
// cleared the published enrichment (Set(Enrichment{})), losing session_id immediately at race end.
func TestOnSessionEnd_RetainsEnrichment(t *testing.T) {
	state := NewEnrichmentState()
	e := NewEnrichSink(testAnalyzer(), state, func(err error) { t.Errorf("unexpected onErr: %v", err) })
	defer e.Close()

	rec := &session.Record{ID: "session-finished", CurrentLap: 0}
	e.OnSessionStart(rec)
	e.OnSessionEnd(rec)

	if got := state.Get().SessionID; got != rec.ID {
		t.Fatalf("SessionID after OnSessionEnd = %q, want retained %q", got, rec.ID)
	}
}

// TestEnrichSink_CloseRace hammers OnFrame/OnSessionEnd (which call request) from goroutines while
// Close runs concurrently, guarding against a send-on-closed-channel panic. Run with -race.
func TestEnrichSink_CloseRace(t *testing.T) {
	state := NewEnrichmentState()
	e := NewEnrichSink(testAnalyzer(), state, nil)

	rec := &session.Record{ID: "session-race", CurrentLap: 0}
	e.OnSessionStart(rec)

	const producers = 10
	var wg sync.WaitGroup
	wg.Add(producers)
	stop := make(chan struct{})
	for i := 0; i < producers; i++ {
		go func(lap int32) {
			defer wg.Done()
			n := int32(1)
			for {
				select {
				case <-stop:
					return
				default:
					e.OnFrame("session-race", &telemetryv1.TelemetryFrame{CurrentLap: lap + n})
					n++
				}
			}
		}(int32(i * 1000))
	}

	time.Sleep(10 * time.Millisecond)
	e.Close()
	close(stop)
	wg.Wait()
}

func (r *recordingLapStore) DominantTracks() map[string]string { return r.inner.DominantTracks() }

func (b *blockingLapStore) DominantTracks() map[string]string { return b.inner.DominantTracks() }

// TestEnrichSink_SessionStartKeepsTrack: the track belongs to the TrackDetector, which resets it at its
// own session start and may publish one at once (a user's pending track pick). The enrich sink's
// session-start reset must not wipe it, whichever of the two sinks runs first.
func TestEnrichSink_SessionStartKeepsTrack(t *testing.T) {
	state := NewEnrichmentState()
	e := NewEnrichSink(analysis.Analyzer{}, state, nil)
	defer e.Close()
	slug, conf := "red-bull-ring", "confident"
	state.SetTrack("s2", &slug, &conf)
	e.OnSessionStart(&session.Record{ID: "s2"})
	got := state.Get()
	if got.SessionID != "s2" || got.Track == nil || *got.Track != slug || got.TrackConfidence == nil || *got.TrackConfidence != conf {
		t.Fatalf("after start: %+v, want s2 with red-bull-ring/confident kept", got)
	}
}
