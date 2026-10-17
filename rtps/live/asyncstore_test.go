package live

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
)

// blockingStore blocks SaveBatch until released, to prove the async wrapper never blocks its caller.
type blockingStore struct {
	release chan struct{}
	mu      sync.Mutex
	saved   int
}

func (b *blockingStore) SaveBatch(s []processing.Sample) error {
	<-b.release
	b.mu.Lock()
	b.saved += len(s)
	b.mu.Unlock()
	return nil
}
func (b *blockingStore) ByLap(string, int32) ([]processing.Sample, error) { return nil, nil }
func (b *blockingStore) DeleteSession(string) error                       { return nil }

func TestAsyncTraceStore_NeverBlocks(t *testing.T) {
	inner := &blockingStore{release: make(chan struct{})}
	var dropped int
	as := NewAsyncTraceStore(inner, 2, func(d int) { dropped += d })
	defer func() { close(inner.release); as.Close() }()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = as.SaveBatch([]processing.Sample{{}})
		}
		close(done)
	}()
	select {
	case <-done: // returned promptly despite inner blocking -> non-blocking confirmed
	case <-time.After(time.Second):
		t.Fatal("SaveBatch blocked on a stalled inner store")
	}
	if dropped == 0 {
		t.Error("expected drops when the writer is stalled and the queue overflows")
	}
}

// TestAsyncTraceStore_ConcurrentProducers proves SaveBatch is safe when called from many goroutines at
// once (staff review defect #2: sinks are shared across concurrent ingest streams, not single-producer).
// Run with -race: any unsynchronized access to the drop-oldest sequence will be flagged.
func TestAsyncTraceStore_ConcurrentProducers(t *testing.T) {
	inner := &blockingStore{release: make(chan struct{})}
	var dropped int64
	as := NewAsyncTraceStore(inner, 4, func(d int) { atomic.AddInt64(&dropped, int64(d)) })
	defer func() { close(inner.release); as.Close() }()

	const producers = 20
	const perProducer = 50
	var wg sync.WaitGroup
	wg.Add(producers)
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				_ = as.SaveBatch([]processing.Sample{{}})
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done: // all producers returned promptly despite the stalled inner store
	case <-time.After(2 * time.Second):
		t.Fatal("SaveBatch blocked under concurrent producers with a stalled inner store")
	}
}

// TestAsyncTraceStore_CloseRace hammers SaveBatch from producer goroutines while Close runs
// concurrently, guarding against the send-on-closed-channel panic (staff review defect #1). Run with
// -race.
func TestAsyncTraceStore_CloseRace(t *testing.T) {
	inner := processing.NewMemTraceStore()
	as := NewAsyncTraceStore(inner, 4, nil)

	const producers = 10
	var wg sync.WaitGroup
	wg.Add(producers)
	stop := make(chan struct{})
	for i := 0; i < producers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = as.SaveBatch([]processing.Sample{{}})
				}
			}
		}()
	}

	// Give producers a moment to start racing with Close, then close and let them wind down.
	time.Sleep(10 * time.Millisecond)
	as.Close()
	close(stop)
	wg.Wait()
}

// gatedStore wraps a MemTraceStore whose SaveBatch waits on gate, so a test can hold the writer
// goroutine mid-write while more batches queue behind it.
type gatedStore struct {
	*processing.MemTraceStore
	gate    chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (g *gatedStore) SaveBatch(s []processing.Sample) error {
	g.once.Do(func() { close(g.entered) })
	<-g.gate
	return g.MemTraceStore.SaveBatch(s)
}

// TestAsyncTraceStore_DeleteSessionDropsQueued: deleting a session removes its stored samples AND any
// of its batches still queued, so a dropped duplicate leaves no trace even with writes in flight.
func TestAsyncTraceStore_DeleteSessionDropsQueued(t *testing.T) {
	inner := &gatedStore{MemTraceStore: processing.NewMemTraceStore(), gate: make(chan struct{}), entered: make(chan struct{})}
	a := NewAsyncTraceStore(inner, 8, nil)
	_ = a.SaveBatch([]processing.Sample{{SessionID: "keep", LapNumber: 1}}) // writer blocks on this
	<-inner.entered
	_ = a.SaveBatch([]processing.Sample{{SessionID: "dup", LapNumber: 1}}) // queued behind it
	done := make(chan error)
	go func() { done <- a.DeleteSession("dup") }()
	close(inner.gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_ = a.SaveBatch([]processing.Sample{{SessionID: "dup", LapNumber: 2}}) // late write after delete
	a.Close()
	for lap := int32(1); lap <= 2; lap++ {
		if got, _ := inner.ByLap("dup", lap); len(got) != 0 {
			t.Errorf("dup lap %d has %d samples after DeleteSession, want 0", lap, len(got))
		}
	}
	if got, _ := inner.ByLap("keep", 1); len(got) != 1 {
		t.Errorf("keep has %d samples, want 1", len(got))
	}
}
