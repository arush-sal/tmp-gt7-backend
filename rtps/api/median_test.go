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

func TestAPI_SessionMedian_NotFound(t *testing.T) {
	srv, _, _, _ := lapsServer(fakeSectors{})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/nope/median", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// TestAPI_SessionMedian serves the median line for a session with two clean racing laps, including the
// discrete gear channel (integer, never averaged).
func TestAPI_SessionMedian(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(fakeSectors{})

	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusFinished})
	// Two laps: gears 3 and 4 -> discrete median = 3 (lower-middle, not 3.5).
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, StartTime: base, Duration: 90 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(medianSamples("s1", 1, base, 90*time.Second, 30, 3))
	base2 := base.Add(2 * time.Minute)
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 2, StartTime: base2, Duration: 90 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(medianSamples("s1", 2, base2, 90*time.Second, 30, 4))

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/median?channels=speed,gear&points=50", nil))
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
	if !ok || len(speed) != 50 {
		t.Fatalf("series[speed] = %v (ok=%v), want len 50", speed, ok)
	}
	if speed[0] != 100 { // medianSamples sets constant Speed 100
		t.Errorf("series[speed][0] = %v, want 100", speed[0])
	}
	gear, ok := got.Series["gear"]
	if !ok {
		t.Fatalf("series missing 'gear': %+v", got.Series)
	}
	// Gears 3 and 4 -> discrete median is an integer 3, NOT the 3.5 average.
	if gear[0] != 3 {
		t.Errorf("series[gear][0] = %v, want 3 (discrete lower-middle, not 3.5)", gear[0])
	}
}

// medianSamples is mkSamples plus a constant gear on every sample.
func medianSamples(sid string, lap int32, t0 time.Time, span time.Duration, n int, gear int32) []processing.Sample {
	out := mkSamples(sid, lap, t0, span, n)
	for i := range out {
		out[i].Gear = gear
	}
	return out
}
