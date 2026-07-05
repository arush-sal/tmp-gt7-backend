package live

import (
	"context"
	"fmt"
	"sync"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// EnrichSink refreshes a session's analysis when a lap completes (or the session ends) and publishes
// it into EnrichmentState, so the live stream's `analysis` fills in per lap without running the heavy
// analysis on every 60Hz frame. It implements session.Sink.
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
// mu guards lap + pending because session.Sink instances are shared across concurrent ingest streams.
type EnrichSink struct {
	analyzer analysis.Analyzer
	state    *EnrichmentState
	onErr    func(error)

	mu      sync.Mutex
	lap     map[string]int32    // last seen current_lap per session, to detect boundaries
	pending map[string]struct{} // sessions awaiting a recompute (per-session coalescing)
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
		lap: map[string]int32{}, pending: map[string]struct{}{},
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

// drain recomputes analysis for every currently-pending session (coalesced) and publishes it.
func (e *EnrichSink) drain() {
	e.mu.Lock()
	ids := make([]string, 0, len(e.pending))
	for id := range e.pending {
		ids = append(ids, id)
	}
	e.pending = map[string]struct{}{}
	e.mu.Unlock()
	for _, id := range ids {
		a, err := e.analyzer.AnalyzeSession(context.Background(), id)
		if err != nil {
			e.onErr(fmt.Errorf("enrich analyze %s: %w", id, err))
			continue
		}
		e.state.Set(Enrichment{SessionID: id, Analysis: &a})
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

// OnSessionStart seeds the lap tracker and resets the published enrichment for the new session.
func (e *EnrichSink) OnSessionStart(rec *session.Record) {
	e.mu.Lock()
	e.lap[rec.ID] = rec.CurrentLap
	e.mu.Unlock()
	e.state.Set(Enrichment{SessionID: rec.ID})
}

// OnFrame detects lap boundaries (current_lap increasing) and requests an async recompute.
func (e *EnrichSink) OnFrame(sessionID string, f *telemetryv1.TelemetryFrame) {
	e.mu.Lock()
	last, ok := e.lap[sessionID]
	boundary := ok && f.GetCurrentLap() > last
	e.lap[sessionID] = f.GetCurrentLap()
	e.mu.Unlock()
	if boundary {
		e.request(sessionID)
	}
}

// OnSessionEnd forgets the session's lap tracker and requests one final recompute.
func (e *EnrichSink) OnSessionEnd(rec *session.Record) {
	e.mu.Lock()
	delete(e.lap, rec.ID)
	e.mu.Unlock()
	e.request(rec.ID)
}

// Close stops the worker after a final drain. Idempotent.
func (e *EnrichSink) Close() {
	e.mu.Lock()
	e.stopped = true
	e.mu.Unlock()
	e.once.Do(func() { close(e.stop) })
	e.wg.Wait()
}
