package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
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

// TestAPI_SessionReference serves the predicted (ideal) lap for a session with two clean racing laps
// over real (2-sector) geometry.
func TestAPI_SessionReference(t *testing.T) {
	sec := fakeSectors{sectors: []reference.Sector{{Index: 0, Start: 0, End: 0.5}, {Index: 1, Start: 0.5, End: 1}}}
	srv, sessions, laps, traces := lapsServer(sec)

	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusFinished})
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, StartTime: base, Duration: 90 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(mkSamples("s1", 1, base, 90*time.Second, 30))
	base2 := base.Add(2 * time.Minute)
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 2, StartTime: base2, Duration: 90 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(mkSamples("s1", 2, base2, 90*time.Second, 30))

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/reference", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var got predictedLapDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rr.Body.String())
	}
	if !got.Reliable || got.Reason != nil {
		t.Errorf("reliable=%v reason=%v, want true/null", got.Reliable, got.Reason)
	}
	if got.PredictedBestNs <= 0 {
		t.Errorf("predicted_best_ns = %d, want > 0", got.PredictedBestNs)
	}
	if len(got.Sectors) != 2 {
		t.Fatalf("sectors = %d, want 2", len(got.Sectors))
	}
	if got.Sectors[0].Label != "S1" {
		t.Errorf("sector0 label = %q, want S1", got.Sectors[0].Label)
	}
	if len(got.Samples) == 0 {
		t.Fatal("samples empty, want spliced trace")
	}
	if got.Samples[0].Fraction != 0 {
		t.Errorf("first sample fraction = %v, want 0", got.Samples[0].Fraction)
	}
	if last := got.Samples[len(got.Samples)-1]; last.Fraction < 0.999 {
		t.Errorf("last sample fraction = %v, want ~1", last.Fraction)
	}
	if got.Samples[0].Steering != 0.25 { // steering reaches the reference DTO
		t.Errorf("sample0 steering = %v, want 0.25", got.Samples[0].Steering)
	}
}

// TestAPI_SessionReference_Decimate caps the samples array and keeps the first + last sample.
func TestAPI_SessionReference_Decimate(t *testing.T) {
	sec := fakeSectors{sectors: []reference.Sector{{Index: 0, Start: 0, End: 1}}}
	srv, sessions, laps, traces := lapsServer(sec)

	base := time.Unix(3000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusFinished})
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, StartTime: base, Duration: 99 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(mkSamples("s1", 1, base, 99*time.Second, 100))

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/reference?decimate=10", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var got predictedLapDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rr.Body.String())
	}
	// 100 spliced samples, decimate=10 -> at most 11 (strided + final).
	if len(got.Samples) > 11 {
		t.Errorf("samples = %d, want <= 11 (decimated)", len(got.Samples))
	}
	if len(got.Samples) < 2 {
		t.Fatalf("samples = %d, want >= 2", len(got.Samples))
	}
	if got.Samples[0].Fraction != 0 {
		t.Errorf("first sample fraction = %v, want 0 (preserved)", got.Samples[0].Fraction)
	}
	if last := got.Samples[len(got.Samples)-1]; last.Fraction < 0.999 {
		t.Errorf("last sample fraction = %v, want ~1 (preserved)", last.Fraction)
	}
}
