package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// fakeAnalyzer satisfies the (unexported) analyzer interface coach.New expects.
type fakeAnalyzer struct{}

func (fakeAnalyzer) AnalyzeSession(_ context.Context, id string, _ time.Duration) (analysis.SessionAnalysis, error) {
	return analysis.SessionAnalysis{SessionID: id, HasPB: true, PBLap: 1, Recommendations: []string{"PB: lap 1"}}, nil
}

func testServer() (*Server, *session.MemStore) {
	store := session.NewMemStore()
	_ = store.Save(&session.Record{ID: "s1", CarID: 1384, CarName: "Nissan Fairlady Z Version S (Z33)", Status: session.StatusFinished})
	return NewServer(coach.New(fakeAnalyzer{}), store, live.New(), live.NewEnrichmentState(), nil, nil, nil, nil, nil, nil), store
}

func TestAPI_ListSessions(t *testing.T) {
	srv, _ := testServer()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got []sessionSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ID != "s1" || got[0].CarID != 1384 {
		t.Errorf("sessions = %+v, want [s1/1384]", got)
	}
	if got[0].CarName != "Nissan Fairlady Z Version S (Z33)" {
		t.Errorf("CarName = %q, want %q", got[0].CarName, "Nissan Fairlady Z Version S (Z33)")
	}
}

// TestAPI_ListSessionsBestLap: best_lap uses the live frame's protobuf-Duration JSON encoding
// ("115.583s") and is null when the session has no best lap (never "0s").
func TestAPI_ListSessionsBestLap(t *testing.T) {
	srv, store := testServer()
	_ = store.Save(&session.Record{ID: "s2", BestLap: 115583 * time.Millisecond, Status: session.StatusFinished})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions", nil))
	var got []map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2", len(got))
	}
	if b, ok := got[0]["best_lap"]; !ok || string(b) != "null" {
		t.Errorf("s1 best_lap = %s (present=%v), want null", b, ok)
	}
	if b := string(got[1]["best_lap"]); b != `"115.583s"` {
		t.Errorf("s2 best_lap = %s, want \"115.583s\"", b)
	}
}

func TestAPI_SessionCoaching(t *testing.T) {
	srv, _ := testServer()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/coaching", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var rep coach.Report
	if err := json.Unmarshal(rr.Body.Bytes(), &rep); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rep.Analysis.SessionID != "s1" || rep.Coaching.Status != "pending" {
		t.Errorf("report = %+v, want analysis s1 + coaching pending", rep)
	}
}

// TestAPI_SessionCoaching_ActiveConflict: an active session's coaching report is gated off (409) —
// its stored Record.BestLap is frozen at confirm-time, so serving from it would show a stale PB while
// the session is still live. The live view at /v1/live is the right place for an in-progress session.
func TestAPI_SessionCoaching_ActiveConflict(t *testing.T) {
	srv, store := testServer()
	_ = store.Save(&session.Record{ID: "active-1", CarID: 1384, Status: session.StatusActive})

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/active-1/coaching", nil))

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
	var e errorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil || e.Error == "" {
		t.Errorf("error body = %q err=%v, want non-empty error shape", rr.Body.String(), err)
	}
}

func TestAPI_SessionCoaching_NotFound(t *testing.T) {
	srv, _ := testServer()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/nope/coaching", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	var e errorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &e); err != nil || e.Error == "" {
		t.Errorf("error body = %q err=%v, want non-empty error shape", rr.Body.String(), err)
	}
}

// TestAPI_ListSessionsTrackSlug: track_slug is the session's dominant confident track, null without one.
func TestAPI_ListSessionsTrackSlug(t *testing.T) {
	store := session.NewMemStore()
	_ = store.Save(&session.Record{ID: "s1", Status: session.StatusFinished})
	_ = store.Save(&session.Record{ID: "s2", Status: session.StatusFinished})
	laps := processing.NewMemLapStore()
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, TrackSlug: "suzuka", TrackConfidence: processing.ConfidenceConfident})
	srv := NewServer(coach.New(fakeAnalyzer{}), store, live.New(), live.NewEnrichmentState(), nil, nil, nil, laps, nil, nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions", nil))
	var got []map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]string{"s1": `"suzuka"`, "s2": `null`}
	for _, row := range got {
		var id string
		_ = json.Unmarshal(row["id"], &id)
		if string(row["track_slug"]) != want[id] {
			t.Errorf("%s track_slug = %s, want %s", id, row["track_slug"], want[id])
		}
	}
	// A server without a lap store (nil) still lists, with null tracks.
	nolaps, _ := testServer()
	rr = httptest.NewRecorder()
	nolaps.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"track_slug":null`) {
		t.Errorf("nil lap store: %d %s", rr.Code, rr.Body.String())
	}
}
