package live

import (
	"sync"
	"testing"

	"github.com/arush-sal/garage/backend/rtps/analysis"
)

func TestEnrichmentState_SetGet(t *testing.T) {
	e := NewEnrichmentState()
	if got := e.Get(); got.Analysis != nil || got.SessionID != "" {
		t.Fatalf("zero-value Get = %+v, want empty", got)
	}
	a := &analysis.SessionAnalysis{SessionID: "sess-1", HasPB: true}
	e.Set(Enrichment{SessionID: "sess-1", Analysis: a})
	got := e.Get()
	if got.SessionID != "sess-1" || got.Analysis == nil || !got.Analysis.HasPB {
		t.Errorf("Get = %+v, want sess-1 with HasPB analysis", got)
	}
}

func TestEnrichmentState_Concurrent(t *testing.T) {
	e := NewEnrichmentState()
	var wg sync.WaitGroup
	numGoroutines := 50
	iterationsPerGoroutine := 100

	// Launch concurrent Set and Get operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterationsPerGoroutine; j++ {
				a := &analysis.SessionAnalysis{SessionID: "sess", HasPB: j%2 == 0}
				e.Set(Enrichment{SessionID: "sess", Analysis: a})
				_ = e.Get() // Just verify Get doesn't panic; no assertion on values
			}
		}(i)
	}
	wg.Wait()
	// If we reach here without data race detected by -race flag, concurrency is safe.
}
