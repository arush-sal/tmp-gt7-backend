package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// fakeAnalyzer satisfies the (unexported) analyzer interface coach.New expects.
type fakeAnalyzer struct{}

func (fakeAnalyzer) AnalyzeSession(_ context.Context, id string) (analysis.SessionAnalysis, error) {
	return analysis.SessionAnalysis{SessionID: id, HasPB: true, PBLap: 1, Recommendations: []string{"PB: lap 1"}}, nil
}

func testServer() (*Server, *session.MemStore) {
	store := session.NewMemStore()
	_ = store.Save(&session.Record{ID: "s1", CarID: 1384, Status: session.StatusFinished})
	return NewServer(coach.New(fakeAnalyzer{}), store, nil), store
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
