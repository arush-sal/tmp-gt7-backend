package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/live"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLiveSnapshot_AliasAndFields(t *testing.T) {
	hub := live.New()
	enrich := live.NewEnrichmentState()
	// coach.Coach is a struct (not an interface), so the brief's literal `nil` for this arg does
	// not compile; the handler under test (handleLive) never touches s.coach, so the zero value is
	// sufficient. session.Store IS an interface, so nil is valid there.
	srv := NewServer(coach.Coach{}, nil, hub, enrich, nil, nil)
	h := srv.Handler()

	// idle first
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("idle status = %d, want 200", rec.Code)
	}

	hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 100, RecvTimestamp: timestamppb.Now()})
	for _, path := range []string{"/v1/live", "/"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s body: %v", path, err)
		}
		if _, ok := body["frame"]; !ok {
			t.Errorf("%s missing frame", path)
		}
	}
}
