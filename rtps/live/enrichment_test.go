package live

import (
	"encoding/json"
	"strings"
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

func TestEnrichmentState_SetAnalysisThenSetTrack_NoClobber(t *testing.T) {
	e := NewEnrichmentState()
	a := &analysis.SessionAnalysis{SessionID: "sess-1", HasPB: true}
	e.SetAnalysis("sess-1", a)

	slug := "laguna-seca"
	confidence := "confident"
	e.SetTrack("sess-1", &slug, &confidence)

	got := e.Get()
	if got.Analysis == nil || !got.Analysis.HasPB {
		t.Errorf("Get().Analysis = %+v, want preserved analysis with HasPB", got.Analysis)
	}
	if got.Track == nil || *got.Track != "laguna-seca" {
		t.Errorf("Get().Track = %v, want laguna-seca", got.Track)
	}
	if got.TrackConfidence == nil || *got.TrackConfidence != "confident" {
		t.Errorf("Get().TrackConfidence = %v, want confident", got.TrackConfidence)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("Get().SessionID = %q, want sess-1", got.SessionID)
	}
}

func TestEnrichmentState_ClearTrack_PreservesAnalysis(t *testing.T) {
	e := NewEnrichmentState()
	a := &analysis.SessionAnalysis{SessionID: "sess-1", HasPB: true}
	e.SetAnalysis("sess-1", a)

	slug := "laguna-seca"
	confidence := "confident"
	e.SetTrack("sess-1", &slug, &confidence)

	e.ClearTrack("sess-1")

	got := e.Get()
	if got.Track != nil {
		t.Errorf("Get().Track = %v, want nil after ClearTrack", got.Track)
	}
	if got.TrackConfidence != nil {
		t.Errorf("Get().TrackConfidence = %v, want nil after ClearTrack", got.TrackConfidence)
	}
	if got.Analysis == nil || !got.Analysis.HasPB {
		t.Errorf("Get().Analysis = %+v, want preserved analysis after ClearTrack", got.Analysis)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("Get().SessionID = %q, want sess-1", got.SessionID)
	}
}

func TestEnrichmentState_ConcurrentSetAnalysisSetTrack_Race(t *testing.T) {
	e := NewEnrichmentState()
	var wg sync.WaitGroup
	numGoroutines := 50
	iterationsPerGoroutine := 100

	slug := "laguna-seca"
	confidence := "confident"

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterationsPerGoroutine; j++ {
				a := &analysis.SessionAnalysis{SessionID: "sess", HasPB: j%2 == 0}
				e.SetAnalysis("sess", a)
				_ = e.Get()
			}
		}(i)
	}
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterationsPerGoroutine; j++ {
				e.SetTrack("sess", &slug, &confidence)
				_ = e.Get()
			}
		}(i)
	}
	wg.Wait()
	// If we reach here without data race detected by -race flag, concurrency is safe.
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

// TestEnrichmentState_SetDuplicate: a duplicate live session points session_id at the ORIGINAL and
// sets duplicate_of, preserving the other fields; duplicate_of is omitted from JSON when unset and a
// fresh Set (new session) clears it.
func TestEnrichmentState_SetDuplicate(t *testing.T) {
	s := NewEnrichmentState()
	track := "red-bull-ring"
	s.SetTrack("dup", &track, nil)
	s.SetDuplicate("orig")
	got := s.Get()
	if got.SessionID != "orig" || got.DuplicateOf != "orig" || got.Track == nil || *got.Track != track {
		t.Fatalf("after SetDuplicate = %+v, want session_id=orig duplicate_of=orig track kept", got)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"duplicate_of":"orig"`) || !strings.Contains(string(b), `"session_id":"orig"`) {
		t.Errorf("JSON = %s, want session_id and duplicate_of = orig", b)
	}
	s.Set(Enrichment{SessionID: "next"})
	b, _ = json.Marshal(s.Get())
	if strings.Contains(string(b), "duplicate_of") {
		t.Errorf("JSON = %s, want duplicate_of omitted for a non-duplicate session", b)
	}
}

// TestEnrichmentState_ClearSession: a discarded (not duplicate) session leaves no live link: session_id
// is "" (the "no session" representation), and its analysis and duplicate_of are cleared; the track
// is kept.
func TestEnrichmentState_ClearSession(t *testing.T) {
	s := NewEnrichmentState()
	track := "suzuka"
	s.SetAnalysis("gone", &analysis.SessionAnalysis{SessionID: "gone"})
	s.SetTrack("gone", &track, nil)
	s.SetDuplicate("orig")
	s.ClearSession()
	got := s.Get()
	if got.SessionID != "" || got.Analysis != nil || got.DuplicateOf != "" || got.Track == nil {
		t.Fatalf("after ClearSession = %+v, want empty session/analysis/duplicate_of, track kept", got)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"session_id":""`) || strings.Contains(string(b), "duplicate_of") {
		t.Errorf("JSON = %s", b)
	}
}
