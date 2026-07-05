package live

import (
	"sync"

	"github.com/arush-sal/garage/backend/rtps/processing"
)

// AsyncTraceStore wraps a TraceStore so SaveBatch never blocks the caller: batches go onto a bounded
// queue drained by a writer goroutine. On overflow it drops the OLDEST batch (freshest data is least
// likely to be lost) and reports it via onDrop — retention is the degradable side, egress is not
// (D-B25). Reads (ByLap) delegate straight through.
//
// Concurrency: SaveBatch is safe for concurrent producers — a mutex serializes the drop-oldest
// sequence (SaveBatch runs per-batch, i.e. per lap boundary / buffer cap, not per frame, so the lock
// is cold). Shutdown signals the writer via a stop channel and never closes the producer-written
// queue, so a late SaveBatch can never panic on a closed channel; after Close it drops silently.
type AsyncTraceStore struct {
	inner   processing.TraceStore
	queue   chan []processing.Sample
	onDrop  func(dropped int)
	mu      sync.Mutex // serializes producers + guards stopped
	stopped bool
	stop    chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
}

// NewAsyncTraceStore wraps inner with a bounded, non-blocking write queue of the given depth (clamped
// to at least 1). onDrop, if nil, is a no-op; otherwise it is called on each overflow eviction. onDrop
// is invoked while the store's producer lock is held, so it MUST be fast and non-reentrant (never call
// back into SaveBatch/Close) — the production hook is a plain structured log.
func NewAsyncTraceStore(inner processing.TraceStore, depth int, onDrop func(int)) *AsyncTraceStore {
	if depth < 1 {
		depth = 1
	}
	if onDrop == nil {
		onDrop = func(int) {}
	}
	a := &AsyncTraceStore{
		inner: inner, queue: make(chan []processing.Sample, depth),
		onDrop: onDrop, stop: make(chan struct{}),
	}
	a.wg.Add(1)
	go a.run()
	return a
}

// run drains the queue until Close signals stop, then flushes whatever remains and exits.
func (a *AsyncTraceStore) run() {
	defer a.wg.Done()
	for {
		select {
		case batch := <-a.queue:
			_ = a.inner.SaveBatch(batch)
		case <-a.stop:
			for { // drain the remainder without blocking
				select {
				case batch := <-a.queue:
					_ = a.inner.SaveBatch(batch)
				default:
					return
				}
			}
		}
	}
}

// SaveBatch enqueues without blocking; on overflow it drops the oldest queued batch to make room for
// this fresher one. Safe under concurrent producers. After Close it drops silently (returns nil).
func (a *AsyncTraceStore) SaveBatch(s []processing.Sample) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return nil
	}
	for {
		select {
		case a.queue <- s:
			return nil
		default:
			select {
			case <-a.queue:
				a.onDrop(1)
			default:
			}
		}
	}
}

// ByLap delegates to the inner store.
func (a *AsyncTraceStore) ByLap(sessionID string, lap int32) ([]processing.Sample, error) {
	return a.inner.ByLap(sessionID, lap)
}

// Close stops accepting writes, flushes queued batches, and stops the writer. Idempotent.
func (a *AsyncTraceStore) Close() {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()
	a.once.Do(func() { close(a.stop) })
	a.wg.Wait()
}
