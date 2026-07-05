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
