package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// fakeSectors is an inline processing.SectorProvider: it returns the same sectors/reason for every
// session (all laps of a session share one track), matching the real KB's per-session resolution.
type fakeSectors struct {
	sectors []reference.Sector
	reason  string
	err     error
}

func (f fakeSectors) SectorsForSession(context.Context, string) ([]reference.Sector, string, error) {
	return f.sectors, f.reason, f.err
}

func (f fakeSectors) SectorsForTrack(context.Context, string) ([]reference.Sector, string, error) {
	return f.sectors, f.reason, f.err
}

// lapsServer wires an API server with a session, lap, and trace store for the lap/trace routes.
func lapsServer(sec processing.SectorProvider) (*Server, *session.MemStore, *processing.MemLapStore, *processing.MemTraceStore) {
	sessions := session.NewMemStore()
	laps := processing.NewMemLapStore()
	traces := processing.NewMemTraceStore()
	srv := NewServer(coach.New(fakeAnalyzer{}), sessions, nil, nil, nil, nil, nil, laps, traces, sec)
	return srv, sessions, laps, traces
}

// mkSamples builds n evenly-spaced samples spanning [t0, t0+span] with a monotone distance axis.
func mkSamples(sid string, lap int32, t0 time.Time, span time.Duration, n int) []processing.Sample {
	out := make([]processing.Sample, n)
	for i := 0; i < n; i++ {
		frac := float64(i) / float64(n-1)
		out[i] = processing.Sample{
			RecvTime:  t0.Add(time.Duration(frac * float64(span))),
			SessionID: sid, LapNumber: lap, Seq: uint64(i),
			Distance: frac * 4000, Speed: 100, PosX: float32(i), PosZ: float32(i * 2), Steering: 0.25,
		}
	}
	return out
}

func TestAPI_SessionLaps_NotFound(t *testing.T) {
	srv, _, _, _ := lapsServer(fakeSectors{})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/nope/laps", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// TestAPI_SessionLaps covers the reliable/unreliable mix within one session: real sectors (provider
// reason ""), lap 1 whose trace spans its duration (reliable) and lap 2 whose trace does not
// (trace-span-mismatch). A PB is set so delta_to_pb is non-null.
func TestAPI_SessionLaps(t *testing.T) {
	sec := fakeSectors{sectors: []reference.Sector{{Index: 0, Start: 0, End: 0.5}, {Index: 1, Start: 0.5, End: 1}}}
	srv, sessions, laps, traces := lapsServer(sec)

	base := time.Unix(1000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", Status: session.StatusActive, BestLap: 90 * time.Second})

	// Lap 1: trace span == duration -> analyzable -> reliable.
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, StartTime: base, Duration: 90 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(mkSamples("s1", 1, base, 90*time.Second, 30))

	// Lap 2: trace span (10s) diverges from duration (90s) -> not analyzable -> trace-span-mismatch.
	base2 := base.Add(2 * time.Minute)
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 2, StartTime: base2, Duration: 90 * time.Second, Complete: true, Timed: true, Valid: true})
	_ = traces.SaveBatch(mkSamples("s1", 2, base2, 10*time.Second, 30))

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/laps", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var got []lapDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rr.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("laps = %d, want 2", len(got))
	}

	l1, l2 := got[0], got[1]
	if l1.LapNumber != 1 || !l1.Sectors.Reliable {
		t.Errorf("lap1 = %+v, want reliable", l1)
	}
	if l1.Sectors.Reason != nil {
		t.Errorf("lap1 reason = %v, want null", *l1.Sectors.Reason)
	}
	if l1.DeltaToPBNs == nil || *l1.DeltaToPBNs != 0 {
		t.Errorf("lap1 delta_to_pb = %v, want 0 (dur==BestLap)", l1.DeltaToPBNs)
	}
	if len(l1.Sectors.Splits) != 2 {
		t.Fatalf("lap1 splits = %d, want 2", len(l1.Sectors.Splits))
	}
	if l1.Sectors.Splits[0].DeltaToBestNs == nil {
		t.Errorf("lap1 split0 delta_to_best = null, want non-null (analyzable pool)")
	}
	if l1.Sectors.Splits[0].Label != "S1" {
		t.Errorf("lap1 split0 label = %q, want S1", l1.Sectors.Splits[0].Label)
	}

	if l2.LapNumber != 2 || l2.Sectors.Reliable {
		t.Errorf("lap2 = %+v, want NOT reliable", l2)
	}
	if l2.Sectors.Reason == nil || *l2.Sectors.Reason != "trace-span-mismatch" {
		t.Errorf("lap2 reason = %v, want trace-span-mismatch", l2.Sectors.Reason)
	}
}

// TestAPI_SessionLaps_NoPBNoSectors: no PB (BestLap 0) and an undetected track (provider reason) with
// a lap whose trace can't be analyzed -> delta_to_pb null and delta_to_best null, reason from provider.
func TestAPI_SessionLaps_NoPBNoSectors(t *testing.T) {
	sec := fakeSectors{reason: "track-not-detected"}
	srv, sessions, laps, traces := lapsServer(sec)

	base := time.Unix(2000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s2", Status: session.StatusFinished}) // BestLap 0
	_ = laps.Save(&processing.Lap{SessionID: "s2", LapNumber: 1, StartTime: base, Duration: 90 * time.Second})
	_ = traces.SaveBatch(mkSamples("s2", 1, base, 10*time.Second, 30)) // span mismatch -> not analyzable

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s2/laps", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var got []lapDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("laps = %d, want 1", len(got))
	}
	l := got[0]
	if l.DeltaToPBNs != nil {
		t.Errorf("delta_to_pb = %v, want null (no PB)", *l.DeltaToPBNs)
	}
	if l.Sectors.Reason == nil || *l.Sectors.Reason != "track-not-detected" {
		t.Errorf("reason = %v, want track-not-detected", l.Sectors.Reason)
	}
	for i, sp := range l.Sectors.Splits {
		if sp.DeltaToBestNs != nil {
			t.Errorf("split %d delta_to_best = %v, want null (no analyzable pool)", i, *sp.DeltaToBestNs)
		}
	}
}

// TestAPI_LapTrace_Channels: the extra channels (fuel/tyre temps/clutch/boost) round-trip into the
// trace DTO, and delta_to_best_ns serializes as a number when set and JSON null when the Sample's
// pointer is nil.
func TestAPI_LapTrace_Channels(t *testing.T) {
	srv, _, laps, traces := lapsServer(fakeSectors{})
	base := time.Unix(4000, 0).UTC()
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, StartTime: base, Duration: time.Second})

	d := int64(777)
	_ = traces.SaveBatch([]processing.Sample{
		{SessionID: "s1", LapNumber: 1, Seq: 0, RecvTime: base,
			Fuel: 42, TyreTempFL: 80, TyreTempFR: 81, TyreTempRL: 82, TyreTempRR: 83,
			Clutch: 0.5, Boost: 1.5, Steering: -0.3, DeltaToBestNs: &d},
		{SessionID: "s1", LapNumber: 1, Seq: 1, RecvTime: base.Add(time.Second),
			Fuel: 41, DeltaToBestNs: nil}, // nil delta -> JSON null
	})

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/laps/1/trace", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	// Assert the raw JSON keys/null explicitly (DTO decode would hide the null-vs-0 distinction).
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v (%s)", err, rr.Body.String())
	}
	if len(raw) != 2 {
		t.Fatalf("samples = %d, want 2", len(raw))
	}
	for _, key := range []string{"fuel", "tyre_temp_fl", "tyre_temp_fr", "tyre_temp_rl", "tyre_temp_rr", "clutch", "boost", "steering", "delta_to_best_ns"} {
		if _, ok := raw[0][key]; !ok {
			t.Errorf("missing key %q in trace DTO", key)
		}
	}

	var got []traceSampleDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode DTO: %v", err)
	}
	s0 := got[0]
	if s0.Fuel != 42 || s0.TyreTempFL != 80 || s0.TyreTempRR != 83 || s0.Clutch != 0.5 || s0.Boost != 1.5 {
		t.Errorf("sample0 channels = %+v, want fuel42/fl80/rr83/clutch0.5/boost1.5", s0)
	}
	if s0.Steering != -0.3 {
		t.Errorf("sample0 steering = %v, want -0.3", s0.Steering)
	}
	if s0.DeltaToBestNs == nil || *s0.DeltaToBestNs != 777 {
		t.Errorf("sample0 delta_to_best_ns = %v, want 777", s0.DeltaToBestNs)
	}
	if string(raw[1]["delta_to_best_ns"]) != "null" {
		t.Errorf("sample1 delta_to_best_ns = %s, want null", raw[1]["delta_to_best_ns"])
	}
}

func TestAPI_LapTrace_BadN(t *testing.T) {
	srv, _, _, _ := lapsServer(fakeSectors{})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/laps/abc/trace", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestAPI_LapTrace_Empty(t *testing.T) {
	srv, _, _, _ := lapsServer(fakeSectors{})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/laps/1/trace", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Body.String(); got != "[]" {
		t.Errorf("body = %q, want []", got)
	}
}

// TestAPI_LapTrace_Decimation: with 100 samples and decimate=10, keep the strided points and always the
// final one; t_ns of the first kept sample is 0 (t0 = lap.StartTime == first sample RecvTime).
func TestAPI_LapTrace_Decimation(t *testing.T) {
	srv, _, laps, traces := lapsServer(fakeSectors{})
	base := time.Unix(3000, 0).UTC()
	_ = laps.Save(&processing.Lap{SessionID: "s1", LapNumber: 1, StartTime: base, Duration: 99 * time.Second})
	_ = traces.SaveBatch(mkSamples("s1", 1, base, 99*time.Second, 100))

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sessions/s1/laps/1/trace?decimate=10", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got []traceSampleDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// stride = ceil(100/10) = 10 -> indices 0,10,...,90 (10) + final index 99 = 11.
	if len(got) != 11 {
		t.Errorf("kept = %d, want 11", len(got))
	}
	if got[0].TNs != 0 {
		t.Errorf("first t_ns = %d, want 0", got[0].TNs)
	}
	if got[len(got)-1].Seq != 99 {
		t.Errorf("last seq = %d, want 99 (final preserved)", got[len(got)-1].Seq)
	}
}
