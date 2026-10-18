package live

import (
	"context"
	"fmt"
	"sync"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// EnrichSink refreshes a session's analysis when a lap completes and publishes it into
// EnrichmentState, so the live stream's `analysis` fills in per lap without running the heavy
// analysis on every 60Hz frame. It implements session.Sink.
//
// Lifecycle: OnSessionStart resets the published enrichment for the new session (correlated fields
// null again). OnFrame's lap-boundary detection requests an async recompute so the live view fills in
// progressively during the session. OnSessionEnd does NOT clear the published enrichment: the finished
// race's enrichment (analysis, track, session_id) is intentionally RETAINED so the live view keeps
// showing post-race context (D-race-lifecycle A1) rather than reverting to null between stints. The
// next OnSessionStart resets it (EnrichmentState.StartSession, which leaves the TrackDetector's track
// alone) when a new session begins.
//
// AnalyzeSession (normalizes traces + splits sectors) runs on a SEPARATE worker goroutine, never on
// the ingest goroutine — recomputing inline would block stream.Recv and stall the live publish cadence
// (publish-first, D-B25). Requests coalesce PER SESSION via a pending set: repeated boundaries for the
// same session collapse to one recompute of its freshest state, and concurrent sessions never evict
// each other. Shutdown signals the worker via a stop channel (no send-on-closed).
//
// Read-after-write note: at a lap boundary the recorder's SaveBatch only ENQUEUES onto AsyncTraceStore,
// so the just-completed lap's trace may not be stored when the worker runs AnalyzeSession. Progressive:
// that lap is simply not sector-analyzable yet (empty sector deltas, handled by the sector guard) and
// its sectors appear on the next refresh once the batch drains; a dropped batch leaves it duration-only.
//
// Active-session fence: the worker's recompute runs OUTSIDE mu (AnalyzeSession is heavy), so a session
// can end while its recompute is in flight. drain re-checks active AFTER AnalyzeSession returns, under
// the same mu that OnSessionEnd uses to fence state (delete from lap/bestLap/active/pending), so a
// publish and the fence are mutually exclusive: an id that ended during analysis is dropped, never
// republished for a session that's no longer active.
//
// The fence is keyed on a GENERATION token, not merely the session ID: session IDs (e.g. "sess-N") are
// minted per-Detector, so they are only unique within one gRPC stream and can collide across streams
// (reconnect, or concurrent collectors sharing one EnrichSink). If generation N's recompute is still in
// flight when a colliding-ID generation N+1 starts, a plain id-presence re-check would pass and publish
// gen-N's stale analysis into gen-N+1's live view. Keying active on id -> generation and requiring the
// SAME generation to still be current at publish time closes that hole.
//
// mu guards lap + pending + active + gen because session.Sink instances are shared across concurrent
// ingest streams.
type EnrichSink struct {
	analyzer analysis.Analyzer
	state    *EnrichmentState
	onErr    func(error)

	mu      sync.Mutex
	lap     map[string]int32         // last seen current_lap per session, to detect boundaries
	bestLap map[string]time.Duration // latest GT7-authoritative best_lap per session, from frames
	pending map[string]struct{}      // sessions awaiting a recompute (per-session coalescing)
	active  map[string]uint64        // session id -> current generation; fences stale/colliding publishes
	gen     uint64                   // monotonic generation counter, bumped on every OnSessionStart
	stopped bool

	notify chan struct{} // 1-deep wakeup for the worker
	stop   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

// NewEnrichSink starts the worker goroutine. onErr, if nil, is a no-op.
func NewEnrichSink(analyzer analysis.Analyzer, state *EnrichmentState, onErr func(error)) *EnrichSink {
	if onErr == nil {
		onErr = func(error) {}
	}
	e := &EnrichSink{
		analyzer: analyzer, state: state, onErr: onErr,
		lap: map[string]int32{}, bestLap: map[string]time.Duration{},
		pending: map[string]struct{}{}, active: map[string]uint64{},
		notify: make(chan struct{}, 1), stop: make(chan struct{}),
	}
	e.wg.Add(1)
	go e.worker()
	return e
}

// worker recomputes analysis for pending sessions off the ingest goroutine until Close signals stop.
func (e *EnrichSink) worker() {
	defer e.wg.Done()
	for {
		select {
		case <-e.notify:
			e.drain()
		case <-e.stop:
			e.drain() // final pass for anything still pending
			return
		}
	}
}

// drain recomputes analysis for every currently-pending, still-active session (coalesced) and
// publishes it. The active re-check after AnalyzeSession fences out sessions that ended while their
// recompute was in flight, AND out sessions whose id was reused by a newer generation in the meantime
// (see the type doc comment): the generation captured at pre-filter must still match the current
// generation for that id at publish time.
func (e *EnrichSink) drain() {
	type work struct {
		id   string
		gen  uint64
		best time.Duration
	}
	e.mu.Lock()
	todo := make([]work, 0, len(e.pending))
	for id := range e.pending {
		if g, ok := e.active[id]; ok { // only recompute still-active sessions
			// Capture the best INTO the work item under the same mu lock as gen — a generation-fence:
			// re-reading e.bestLap[id] after AnalyzeSession returns would risk observing a value written
			// by a colliding newer generation for the same id (see the type doc comment on gen).
			todo = append(todo, work{id: id, gen: g, best: e.bestLap[id]})
		}
	}
	e.pending = map[string]struct{}{}
	e.mu.Unlock()
	for _, w := range todo {
		a, err := e.analyzer.AnalyzeSession(context.Background(), w.id, w.best)
		if err != nil {
			e.onErr(fmt.Errorf("enrich analyze %s: %w", w.id, err))
			continue
		}
		e.mu.Lock()
		if e.active[w.id] == w.gen { // same session generation still active? (guards ID collisions across streams)
			e.state.SetAnalysis(w.id, &a)
		}
		e.mu.Unlock()
	}
}

// request marks a session for recompute (non-blocking) and wakes the worker. After Close it no-ops.
func (e *EnrichSink) request(sessionID string) {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.pending[sessionID] = struct{}{}
	e.mu.Unlock()
	select {
	case e.notify <- struct{}{}:
	default:
	}
}

// OnSessionStart seeds the lap tracker, bumps the generation counter and records it as the current
// generation for this session id, and resets the published enrichment for the new session. Bumping the
// generation even on an id collision (a reused "sess-N" from a different Detector/stream) ensures any
// recompute still in flight for the prior occupant of that id is fenced out by drain's generation check.
func (e *EnrichSink) OnSessionStart(rec *session.Record) {
	e.mu.Lock()
	e.lap[rec.ID] = rec.CurrentLap
	e.bestLap[rec.ID] = 0
	e.gen++
	e.active[rec.ID] = e.gen
	e.mu.Unlock()
	e.state.StartSession(rec.ID)
}

// OnFrame detects lap boundaries (current_lap increasing) and requests an async recompute. It also
// tracks the frame's GT7-authoritative best_lap under mu, so drain can thread it into AnalyzeSession
// as the session's Personal Best (the game's own record, not a value we'd have to infer from stored
// laps).
func (e *EnrichSink) OnFrame(sessionID string, f *telemetryv1.TelemetryFrame) {
	e.mu.Lock()
	last, ok := e.lap[sessionID]
	boundary := ok && f.GetCurrentLap() > last
	e.lap[sessionID] = f.GetCurrentLap()
	e.bestLap[sessionID] = f.GetBestLap().AsDuration() // nil-safe: yields 0 when unset
	e.mu.Unlock()
	if boundary {
		e.request(sessionID)
	}
}

// OnSessionEnd forgets the session's lap tracker and fences it out of pending/active, so an in-flight
// recompute for this session that finishes after this call sees the session no longer active and drops
// its publish. The published live enrichment is intentionally RETAINED (not cleared): the finished
// race's analysis/track/session_id stays visible for post-race review until the next OnSessionStart
// overwrites it for the new session.
func (e *EnrichSink) OnSessionEnd(rec *session.Record) {
	e.mu.Lock()
	delete(e.lap, rec.ID)
	delete(e.bestLap, rec.ID)
	delete(e.active, rec.ID)
	delete(e.pending, rec.ID)
	e.mu.Unlock()
}

// Close stops the worker after a final drain. Idempotent.
func (e *EnrichSink) Close() {
	e.mu.Lock()
	e.stopped = true
	e.mu.Unlock()
	e.once.Do(func() { close(e.stop) })
	e.wg.Wait()
}
