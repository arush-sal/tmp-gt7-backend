package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/session"
)

func TestAPI_SessionReference_NotFound(t *testing.T) {
	srv, _, _, _ := lapsServer(fakeSectors{})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/nope/reference", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// TestAPI_SessionReference serves the median reference for a session with two clean racing laps.
func TestAPI_SessionReference(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(fakeSectors{})

	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusFinished})
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, StartTime: base, Duration: 90 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(mkSamples("s1", 1, base, 90*time.Second, 30))
	base2 := base.Add(2 * time.Minute)
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 2, StartTime: base2, Duration: 90 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(mkSamples("s1", 2, base2, 90*time.Second, 30))

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/reference?channels=speed&points=50", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var got processing.ReferenceSeries
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rr.Body.String())
	}
	if got.LapCount != 2 {
		t.Errorf("lap_count = %d, want 2", got.LapCount)
	}
	if len(got.Fractions) != 50 {
		t.Errorf("fractions len = %d, want 50", len(got.Fractions))
	}
	speed, ok := got.Series["speed"]
	if !ok {
		t.Fatalf("series missing 'speed': %+v", got.Series)
	}
	if len(speed) != 50 {
		t.Errorf("series[speed] len = %d, want 50", len(speed))
	}
	// mkSamples sets a constant Speed of 100 -> median 100 everywhere.
	if speed[0] != 100 {
		t.Errorf("series[speed][0] = %v, want 100", speed[0])
	}
}
