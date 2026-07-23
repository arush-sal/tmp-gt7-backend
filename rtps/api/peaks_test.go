package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// peaksResp mirrors peaksDTO for decoding. Pointer fields decode to nil on JSON null, so the trust
// axes and delta_ns null-vs-value distinctions are checked directly.
type peaksResp struct {
	Reliable  bool    `json:"reliable"`
	Reason    *string `json:"reason"`
	Reference struct {
		Kind      string `json:"kind"`
		LapNumber *int32 `json:"lap_number"`
	} `json:"reference"`
	ReferenceReliable bool    `json:"reference_reliable"`
	ReferenceReason   *string `json:"reference_reason"`
	Points            []struct {
		Index    int     `json:"index"`
		Kind     string  `json:"kind"`
		Fraction float64 `json:"fraction"`
		Distance float64 `json:"distance"`
		Speed    float32 `json:"speed"`
		DeltaNs  *int64  `json:"delta_ns"`
	} `json:"points"`
}

// sineSamples builds n evenly-spaced samples over [t0, t0+span] with a sinusoidal speed profile
// (`cycles` full waves) so SpeedExtrema finds robust, high-prominence peaks & valleys. Distance is a
// monotone 0..4000 axis; the first sample is at distance 0 (the lap-start invariant LapElapsed needs).
func sineSamples(sid string, lap int32, t0 time.Time, span time.Duration, n, cycles int) []processing.Sample {
	out := make([]processing.Sample, n)
	for i := range n {
		frac := float64(i) / float64(n-1)
		out[i] = processing.Sample{
			RecvTime:  t0.Add(time.Duration(frac * float64(span))),
			SessionID: sid, LapNumber: lap, Seq: uint64(i),
			Distance: frac * 4000,
			Speed:    float32(150 + 60*math.Sin(2*math.Pi*float64(cycles)*frac)),
			PosX:     float32(i), PosZ: float32(i * 2),
		}
	}
	return out
}

// realSectors is a two-sector track split with real geometry (reason "" -> predicted lap reliable).
func realSectors() fakeSectors {
	return fakeSectors{sectors: []reference.Sector{{Index: 0, Start: 0, End: 0.5}, {Index: 1, Start: 0.5, End: 1}}}
}

func racingLapRec(sid string, n int32, t0 time.Time, dur time.Duration) *processing.Lap {
	return &processing.Lap{SessionID: sid, LapNumber: n, StartTime: t0, Duration: dur, Complete: true, Timed: true, Valid: true}
}

func getPeaks(t *testing.T, srv *Server, path string) (*httptest.ResponseRecorder, peaksResp) {
	t.Helper()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	var got peaksResp
	if rr.Body.Len() > 0 {
		_ = json.Unmarshal(rr.Body.Bytes(), &got)
	}
	return rr, got
}

// TestPeaksHandler_OK: default reference A (predicted-best). A racing lap with a sinusoidal trace ->
// 200, reliable, strictly alternating peaks/valleys, predicted reference, every delta_ns populated.
func TestPeaksHandler_OK(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(realSectors())
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 1, base, 90*time.Second))
	_ = traces.SaveBatch(sineSamples("s1", 1, base, 90*time.Second, 240, 2))

	rr, got := getPeaks(t, srv, "/v1/sessions/s1/laps/1/peaks")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rr.Code, rr.Body.String())
	}
	if !got.Reliable || got.Reason != nil {
		t.Errorf("reliable=%v reason=%v, want true/nil", got.Reliable, got.Reason)
	}
	if got.Reference.Kind != "predicted" || got.Reference.LapNumber != nil {
		t.Errorf("reference=%+v, want predicted/nil", got.Reference)
	}
	if !got.ReferenceReliable || got.ReferenceReason != nil {
		t.Errorf("reference_reliable=%v reason=%v, want true/nil", got.ReferenceReliable, got.ReferenceReason)
	}
	if len(got.Points) < 2 {
		t.Fatalf("got %d points, want >=2: %+v", len(got.Points), got.Points)
	}
	for i, p := range got.Points {
		if p.DeltaNs == nil {
			t.Errorf("point %d: delta_ns null, want populated (reference reliable)", i)
		}
		if i > 0 && p.Kind == got.Points[i-1].Kind {
			t.Errorf("points %d,%d both %q — not alternating", i-1, i, p.Kind)
		}
	}
}

func TestPeaksHandler_UnknownSession404(t *testing.T) {
	srv, _, _, _ := lapsServer(realSectors())
	rr, _ := getPeaks(t, srv, "/v1/sessions/nope/laps/1/peaks")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rr.Code)
	}
}

func TestPeaksHandler_BadLapNumber400(t *testing.T) {
	srv, sessions, _, _ := lapsServer(realSectors())
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	rr, _ := getPeaks(t, srv, "/v1/sessions/s1/laps/abc/peaks")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rr.Code)
	}
}

func TestPeaksHandler_UnknownLap404(t *testing.T) {
	srv, sessions, _, _ := lapsServer(realSectors())
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	rr, _ := getPeaks(t, srv, "/v1/sessions/s1/laps/99/peaks")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 (lap not in session)", rr.Code)
	}
}

func TestPeaksHandler_EmptyTrace404(t *testing.T) {
	srv, sessions, laps, _ := lapsServer(realSectors())
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 2, base, 90*time.Second)) // lap exists, no trace saved
	rr, _ := getPeaks(t, srv, "/v1/sessions/s1/laps/2/peaks")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 (lap trace not found)", rr.Code)
	}
}

// TestPeaksHandler_UntrustedReliableFalse: the lap and its trace exist but the trace span does not match
// the lap duration -> 200 with reliable:false + a non-null reason (present data, not a 404).
func TestPeaksHandler_UntrustedReliableFalse(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(realSectors())
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 1, base, 90*time.Second))
	_ = traces.SaveBatch(sineSamples("s1", 1, base, 5*time.Second, 240, 2)) // 5s span vs 90s duration
	rr, got := getPeaks(t, srv, "/v1/sessions/s1/laps/1/peaks")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	if got.Reliable || got.Reason == nil {
		t.Errorf("reliable=%v reason=%v, want false/non-nil", got.Reliable, got.Reason)
	}
}

// TestPeaksHandler_NoPredictedReferenceDeltaNull: the predicted reference is unusable (mini-sector
// fallback -> Reliable false) -> 200, reference.kind predicted, reference_reliable false, all deltas
// null. The primary extrema are still served.
func TestPeaksHandler_NoPredictedReferenceDeltaNull(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(fakeSectors{
		sectors: []reference.Sector{{Index: 0, Start: 0, End: 0.5}, {Index: 1, Start: 0.5, End: 1}},
		reason:  "no rkb geometry — mini-sector fallback",
	})
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 1, base, 90*time.Second))
	_ = traces.SaveBatch(sineSamples("s1", 1, base, 90*time.Second, 240, 2))
	rr, got := getPeaks(t, srv, "/v1/sessions/s1/laps/1/peaks")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	if got.Reference.Kind != "predicted" {
		t.Errorf("reference.kind=%q, want predicted", got.Reference.Kind)
	}
	if got.ReferenceReliable || got.ReferenceReason == nil {
		t.Errorf("reference_reliable=%v reason=%v, want false/non-nil", got.ReferenceReliable, got.ReferenceReason)
	}
	for i, p := range got.Points {
		if p.DeltaNs != nil {
			t.Errorf("point %d: delta_ns=%d, want null (no usable reference)", i, *p.DeltaNs)
		}
	}
}

// TestPeaksHandler_ReferenceLapOK: ?reference=2 -> reference.kind lap, lap_number 2, populated deltas.
func TestPeaksHandler_ReferenceLapOK(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(realSectors())
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 1, base, 90*time.Second))
	_ = laps.Save(racingLapRec("s1", 2, base, 92*time.Second))
	_ = traces.SaveBatch(sineSamples("s1", 1, base, 90*time.Second, 240, 2))
	_ = traces.SaveBatch(sineSamples("s1", 2, base, 92*time.Second, 240, 3)) // different profile

	rr, got := getPeaks(t, srv, "/v1/sessions/s1/laps/1/peaks?reference=2")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rr.Code, rr.Body.String())
	}
	if got.Reference.Kind != "lap" || got.Reference.LapNumber == nil || *got.Reference.LapNumber != 2 {
		t.Fatalf("reference=%+v, want lap/2", got.Reference)
	}
	if !got.ReferenceReliable {
		t.Errorf("reference_reliable=false, want true")
	}
	for i, p := range got.Points {
		if p.DeltaNs == nil {
			t.Errorf("point %d: delta_ns null, want populated", i)
		}
	}
}

func TestPeaksHandler_ReferenceLapNotFound404(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(realSectors())
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 1, base, 90*time.Second))
	_ = traces.SaveBatch(sineSamples("s1", 1, base, 90*time.Second, 240, 2))
	rr, _ := getPeaks(t, srv, "/v1/sessions/s1/laps/1/peaks?reference=99")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404 (reference lap not found)", rr.Code)
	}
}

func TestPeaksHandler_ReferenceMalformed400(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(realSectors())
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 1, base, 90*time.Second))
	_ = traces.SaveBatch(sineSamples("s1", 1, base, 90*time.Second, 240, 2))
	rr, _ := getPeaks(t, srv, "/v1/sessions/s1/laps/1/peaks?reference=abc")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 (invalid reference)", rr.Code)
	}
}

// TestPeaksHandler_ReferenceLapUntrusted: the named reference lap exists but has no analyzable trace ->
// 200 with reference_reliable false and null deltas (a secondary input degrades, it does not nuke the
// primary extrema).
func TestPeaksHandler_ReferenceLapUntrusted(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(realSectors())
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 1, base, 90*time.Second))
	_ = laps.Save(racingLapRec("s1", 2, base, 90*time.Second)) // reference lap exists...
	_ = traces.SaveBatch(sineSamples("s1", 1, base, 90*time.Second, 240, 2))
	_ = traces.SaveBatch(sineSamples("s1", 2, base, 5*time.Second, 240, 2)) // ...but its trace is not analyzable

	rr, got := getPeaks(t, srv, "/v1/sessions/s1/laps/1/peaks?reference=2")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	if got.Reference.Kind != "lap" || got.ReferenceReliable || got.ReferenceReason == nil {
		t.Errorf("reference=%+v reliable=%v reason=%v, want lap/false/non-nil", got.Reference, got.ReferenceReliable, got.ReferenceReason)
	}
	for i, p := range got.Points {
		if p.DeltaNs != nil {
			t.Errorf("point %d: delta_ns=%d, want null", i, *p.DeltaNs)
		}
	}
}

// TestPeaksHandler_SelfReferenceAllZero: ?reference={queried lap} -> every delta_ns is exactly 0.
func TestPeaksHandler_SelfReferenceAllZero(t *testing.T) {
	srv, sessions, laps, traces := lapsServer(realSectors())
	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive})
	_ = laps.Save(racingLapRec("s1", 1, base, 90*time.Second))
	_ = traces.SaveBatch(sineSamples("s1", 1, base, 90*time.Second, 240, 2))
	rr, got := getPeaks(t, srv, "/v1/sessions/s1/laps/1/peaks?reference=1")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	if got.Reference.Kind != "lap" || got.Reference.LapNumber == nil || *got.Reference.LapNumber != 1 {
		t.Fatalf("reference=%+v, want lap/1", got.Reference)
	}
	for i, p := range got.Points {
		if p.DeltaNs == nil || *p.DeltaNs != 0 {
			t.Errorf("point %d: delta_ns=%v, want 0 (self-reference)", i, p.DeltaNs)
		}
	}
}
