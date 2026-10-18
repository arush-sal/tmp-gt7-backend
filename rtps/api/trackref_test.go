package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// trackRefServer wires the track reference routes over memory stores with real (2-sector) RKB sectors.
// changed records every OnReferenceChange call.
func trackRefServer(t *testing.T) (*Server, *session.MemStore, *processing.MemLapStore, *[]string) {
	t.Helper()
	sec := fakeSectors{sectors: []reference.Sector{{Index: 0, Start: 0, End: 0.5}, {Index: 1, Start: 0.5, End: 1}}}
	srv, sessions, laps, traces := lapsServer(sec)
	srv.TrackRefs = laps
	laps.SessionTotalLaps = func(id string) int32 {
		if r, ok := sessions.Get(id); ok {
			return r.TotalLaps
		}
		return 0
	}
	changed := &[]string{}
	srv.OnReferenceChange = func(slug string) { *changed = append(*changed, slug) }

	base := time.Unix(1_700_000_000, 0).UTC()
	_ = sessions.Save(&session.Record{ID: "s1", CarID: 7, CarName: "Car Seven", CarCategory: "GR3", Status: session.StatusFinished})
	_ = sessions.Save(&session.Record{ID: "s2", CarID: 9, Status: session.StatusFinished}) // no name, no category
	add := func(sid string, n int32, start time.Time, dur time.Duration, valid bool, slug, conf string) {
		_ = laps.Save(&processing.Lap{SessionID: sid, LapNumber: n, StartTime: start, Duration: dur,
			Timed: true, Complete: valid, Valid: valid, TrackSlug: slug, TrackConfidence: conf})
		_ = traces.SaveBatch(mkSamples(sid, n, start, dur, 30))
	}
	add("s1", 1, base, 100*time.Second, true, "suzuka", "confident")
	add("s1", 2, base.Add(2*time.Minute), 98500*time.Millisecond, true, "suzuka", "confident")
	add("s1", 3, base.Add(4*time.Minute), 90*time.Second, false, "suzuka", "confident")  // invalid
	add("s1", 4, base.Add(6*time.Minute), 91*time.Second, true, "suzuka", "provisional") // not confident
	add("s2", 1, base.Add(time.Hour), 99*time.Second, true, "suzuka", "confident")       // other car
	add("s2", 2, base.Add(time.Hour+2*time.Minute), 70*time.Second, true, "red-bull-ring", "confident")
	return srv, sessions, laps, changed
}

// trackRefBody is the GET/PUT/DELETE reference-lap body (the ReferenceDTO fields plus the source).
type trackRefBody struct {
	Reliable        bool                 `json:"reliable"`
	Reason          *string              `json:"reason"`
	PredictedBestNs int64                `json:"predicted_best_ns"`
	Sectors         []predictedSectorDTO `json:"sectors"`
	Samples         []predictedSampleDTO `json:"samples"`
	Source          string               `json:"source"`
	SessionID       string               `json:"session_id"`
	LapNumber       int32                `json:"lap_number"`
	LapTime         string               `json:"lap_time"`
	RecordedAt      string               `json:"recorded_at"`
	Car             struct {
		ID       int32   `json:"id"`
		Name     *string `json:"name"`
		Category *string `json:"category"`
	} `json:"car"`
}

func doJSON(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost:8080" // mutating routes only accept loopback / allow-listed hosts
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

func decodeRef(t *testing.T, rr *httptest.ResponseRecorder) trackRefBody {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d %s, want 200", rr.Code, rr.Body.String())
	}
	var b trackRefBody
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestAPI_TrackReferenceLap_Get: the fastest eligible stored lap across sessions and cars, on the
// ReferenceDTO shape with its provenance; decimation; 404 for a track with nothing stored.
func TestAPI_TrackReferenceLap_Get(t *testing.T) {
	srv, _, _, _ := trackRefServer(t)

	b := decodeRef(t, doJSON(t, srv, http.MethodGet, "/v1/tracks/suzuka/reference-lap", ""))
	if b.Source != "fastest" || b.SessionID != "s1" || b.LapNumber != 2 || b.LapTime != "98.500s" {
		t.Errorf("provenance = %s %s#%d %s, want fastest s1#2 98.500s", b.Source, b.SessionID, b.LapNumber, b.LapTime)
	}
	if !b.Reliable || b.Reason != nil || b.PredictedBestNs != (98500*time.Millisecond).Nanoseconds() {
		t.Errorf("reliable=%v reason=%v predicted_best_ns=%d", b.Reliable, b.Reason, b.PredictedBestNs)
	}
	if len(b.Sectors) != 2 || b.Sectors[0].OwnerLap != 2 || len(b.Samples) != 30 || b.Samples[29].Fraction != 1 {
		t.Errorf("sectors=%+v samples=%d", b.Sectors, len(b.Samples))
	}
	if b.Car.ID != 7 || b.Car.Name == nil || *b.Car.Name != "Car Seven" || b.Car.Category == nil || *b.Car.Category != "GR3" {
		t.Errorf("car = %+v", b.Car)
	}
	if _, err := time.Parse(time.RFC3339, b.RecordedAt); err != nil {
		t.Errorf("recorded_at %q: %v", b.RecordedAt, err)
	}

	if b := decodeRef(t, doJSON(t, srv, http.MethodGet, "/v1/tracks/suzuka/reference-lap?decimate=5", "")); len(b.Samples) > 6 {
		t.Errorf("decimate=5 -> %d samples", len(b.Samples))
	}

	rr := doJSON(t, srv, http.MethodGet, "/v1/tracks/nowhere/reference-lap", "")
	if rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), `"error":"no stored reference for track"`) {
		t.Errorf("unknown track = %d %s, want 404", rr.Code, rr.Body.String())
	}
}

// TestAPI_TrackReferenceLap_Unreliable: no RKB sector data -> mini-sectors with reliable=false + reason.
func TestAPI_TrackReferenceLap_Unreliable(t *testing.T) {
	srv, _, _, _ := trackRefServer(t)
	srv.sectors = fakeSectors{reason: "no-sector-labels"}
	b := decodeRef(t, doJSON(t, srv, http.MethodGet, "/v1/tracks/suzuka/reference-lap", ""))
	if b.Reliable || b.Reason == nil || *b.Reason != "no-sector-labels" || len(b.Sectors) != 3 {
		t.Errorf("reliable=%v reason=%v sectors=%d, want false/no-sector-labels/3", b.Reliable, b.Reason, len(b.Sectors))
	}
}

// TestAPI_TrackReferenceLap_PinUnpin: PUT pins an eligible lap (200, source pinned, GET agrees, the live
// loader is told); every ineligible lap is 409, a missing one 404, a bad body 400; DELETE falls back to
// the fastest lap (200), then 404 once the track has nothing at all.
func TestAPI_TrackReferenceLap_PinUnpin(t *testing.T) {
	srv, _, laps, changed := trackRefServer(t)
	const path = "/v1/tracks/suzuka/reference-lap"

	for _, body := range []string{`nope`, `{}`, `{"session_id": "s1"}`, `{"lap_number": 1}`, `{"session_id": "", "lap_number": 1}`} {
		if rr := doJSON(t, srv, http.MethodPut, path, body); rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want 400", body, rr.Code)
		}
	}
	if rr := doJSON(t, srv, http.MethodPut, path, `{"session_id": "s1", "lap_number": 42}`); rr.Code != http.StatusNotFound {
		t.Errorf("PUT missing lap = %d, want 404", rr.Code)
	}
	for _, body := range []string{
		`{"session_id": "s1", "lap_number": 3}`, // invalid
		`{"session_id": "s1", "lap_number": 4}`, // provisional
		`{"session_id": "s2", "lap_number": 2}`, // another track
	} {
		rr := doJSON(t, srv, http.MethodPut, path, body)
		if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), `"error":"lap not usable as reference"`) {
			t.Errorf("PUT %s = %d %s, want 409", body, rr.Code, rr.Body.String())
		}
	}
	if len(*changed) != 0 {
		t.Fatalf("rejected pins notified the loader: %v", *changed)
	}

	b := decodeRef(t, doJSON(t, srv, http.MethodPut, path, `{"session_id": "s2", "lap_number": 1}`))
	if b.Source != "pinned" || b.SessionID != "s2" || b.LapNumber != 1 || b.Car.Name != nil || b.Car.Category != nil {
		t.Errorf("PUT body = %s %s#%d car=%+v, want pinned s2#1 with null name/category", b.Source, b.SessionID, b.LapNumber, b.Car)
	}
	if b := decodeRef(t, doJSON(t, srv, http.MethodGet, path, "")); b.Source != "pinned" || b.SessionID != "s2" {
		t.Errorf("GET after pin = %s %s", b.Source, b.SessionID)
	}

	b = decodeRef(t, doJSON(t, srv, http.MethodDelete, path, ""))
	if b.Source != "fastest" || b.SessionID != "s1" || b.LapNumber != 2 {
		t.Errorf("DELETE body = %s %s#%d, want fastest s1#2", b.Source, b.SessionID, b.LapNumber)
	}
	if strings.Join(*changed, ",") != "suzuka,suzuka" {
		t.Errorf("loader notified %v, want [suzuka suzuka]", *changed)
	}

	_ = laps.DeleteSession("s1")
	_ = laps.DeleteSession("s2")
	if rr := doJSON(t, srv, http.MethodDelete, path, ""); rr.Code != http.StatusNotFound {
		t.Errorf("DELETE with nothing stored = %d, want 404", rr.Code)
	}
}

// TestAPI_TrackLaps: the track's stored eligible laps fastest first, across sessions and cars, with the
// pinned flag and ?limit.
func TestAPI_TrackLaps(t *testing.T) {
	srv, _, _, _ := trackRefServer(t)
	_ = doJSON(t, srv, http.MethodPut, "/v1/tracks/suzuka/reference-lap", `{"session_id": "s1", "lap_number": 1}`)

	type row struct {
		SessionID  string `json:"session_id"`
		LapNumber  int32  `json:"lap_number"`
		LapTime    string `json:"lap_time"`
		RecordedAt string `json:"recorded_at"`
		Car        struct {
			ID   int32   `json:"id"`
			Name *string `json:"name"`
		} `json:"car"`
		Pinned bool `json:"pinned"`
	}
	get := func(path string) []row {
		rr := doJSON(t, srv, http.MethodGet, path, "")
		if rr.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, rr.Code)
		}
		var rows []row
		if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	rows := get("/v1/tracks/suzuka/laps")
	if len(rows) != 3 || rows[0].LapTime != "98.500s" || rows[1].SessionID != "s2" || rows[2].LapNumber != 1 {
		t.Fatalf("rows = %+v, want s1#2, s2#1, s1#1", rows)
	}
	if rows[0].Pinned || !rows[2].Pinned || rows[1].Car.ID != 9 || rows[1].Car.Name != nil {
		t.Errorf("pinned/car = %+v", rows)
	}
	// The pinned lap is always listed, even outside the top N.
	if rows := get("/v1/tracks/suzuka/laps?limit=1"); len(rows) != 2 || rows[0].LapNumber != 2 || rows[0].Pinned ||
		rows[1].SessionID != "s1" || rows[1].LapNumber != 1 || !rows[1].Pinned {
		t.Errorf("limit=1 with the pin outside it -> %+v, want s1#2 then pinned s1#1", rows)
	}
	if rows := get("/v1/tracks/suzuka/laps?limit=2"); len(rows) != 3 {
		t.Errorf("limit=2 -> %d rows, want 2 + the pinned lap", len(rows))
	}
	if rows := get("/v1/tracks/suzuka/laps?limit=100000"); len(rows) != 3 { // capped at maxTrackLaps, all 3 fit
		t.Errorf("huge limit -> %d rows", len(rows))
	}
	if rr := doJSON(t, srv, http.MethodGet, "/v1/tracks/nowhere/laps", ""); rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Errorf("no laps = %d %s, want 200 []", rr.Code, rr.Body.String())
	}
}

// TestAPI_SessionLaps_TrackFields: /v1/sessions/{id}/laps rows carry each lap's track, null when none.
func TestAPI_SessionLaps_TrackFields(t *testing.T) {
	srv, _, laps, _ := trackRefServer(t)
	_ = laps.Save(&processing.Lap{SessionID: "s2", LapNumber: 3, Duration: time.Minute})
	rr := doJSON(t, srv, http.MethodGet, "/v1/sessions/s2/laps", "")
	var rows []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[1]["track_slug"] != "red-bull-ring" || rows[1]["track_confidence"] != "confident" {
		t.Errorf("lap 2 = %v", rows[1])
	}
	if v, ok := rows[2]["track_slug"]; !ok || v != nil {
		t.Errorf("unlabelled lap track_slug = %v (present %v), want null", v, ok)
	}
	if v, ok := rows[2]["track_confidence"]; !ok || v != nil {
		t.Errorf("unlabelled lap track_confidence = %v (present %v), want null", v, ok)
	}
}

// raceLap1Server adds a 10-lap race (s9) whose lap 1 is the fastest lap on suzuka.
func raceLap1Server(t *testing.T) *Server {
	t.Helper()
	srv, sessions, laps, _ := trackRefServer(t)
	_ = sessions.Save(&session.Record{ID: "s9", CarID: 7, TotalLaps: 10, Status: session.StatusFinished})
	base := time.Unix(1_700_100_000, 0).UTC()
	// Saved in lap order, as the segmenter does: the memory store keeps insertion order, and a map
	// here would randomise it (Go map iteration), flaking the session-laps row-order assertion.
	for _, l := range []struct {
		n int32
		d time.Duration
	}{{1, 60 * time.Second}, {2, 97 * time.Second}} {
		_ = laps.Save(&processing.Lap{SessionID: "s9", LapNumber: l.n, StartTime: base.Add(time.Duration(l.n) * 3 * time.Minute), Duration: l.d,
			Timed: true, Complete: true, Valid: true, TrackSlug: "suzuka", TrackConfidence: "confident"})
	}
	return srv
}

// TestAPI_RaceLap1NotReference: a race's lap 1 is never the fastest reference, cannot be pinned (409),
// is not listed in the track's laps, and is reference_eligible=false in its session's laps.
func TestAPI_RaceLap1NotReference(t *testing.T) {
	srv := raceLap1Server(t)
	const path = "/v1/tracks/suzuka/reference-lap"
	if b := decodeRef(t, doJSON(t, srv, http.MethodGet, path, "")); b.SessionID != "s9" || b.LapNumber != 2 {
		t.Errorf("reference = %s#%d, want s9#2 (not the faster race lap 1)", b.SessionID, b.LapNumber)
	}
	rr := doJSON(t, srv, http.MethodPut, path, `{"session_id": "s9", "lap_number": 1}`)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), `"error":"lap not usable as reference"`) {
		t.Errorf("PUT race lap 1 = %d %s, want 409", rr.Code, rr.Body.String())
	}
	var tl []trackLapDTO
	if err := json.Unmarshal(doJSON(t, srv, http.MethodGet, "/v1/tracks/suzuka/laps", "").Body.Bytes(), &tl); err != nil {
		t.Fatal(err)
	}
	for _, l := range tl {
		if l.SessionID == "s9" && l.LapNumber == 1 {
			t.Errorf("track laps list race lap 1: %+v", l)
		}
	}
	var rows []map[string]any
	if err := json.Unmarshal(doJSON(t, srv, http.MethodGet, "/v1/sessions/s9/laps", "").Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0]["reference_eligible"] != false || rows[1]["reference_eligible"] != true {
		t.Errorf("reference_eligible = %v / %v, want false / true", rows[0]["reference_eligible"], rows[1]["reference_eligible"])
	}
}

// TestAPI_PinnedRaceLap1Removed: a pin on a race's lap 1 (made before the total was known) is removed on
// resolution, so GET falls back to the fastest lap with source "fastest" and a later DELETE has nothing
// to clear.
func TestAPI_PinnedRaceLap1Removed(t *testing.T) {
	srv, sessions, laps, _ := trackRefServer(t)
	_ = sessions.Save(&session.Record{ID: "s9", CarID: 7, Status: session.StatusActive}) // total not yet known
	_ = laps.Save(&processing.Lap{SessionID: "s9", LapNumber: 1, StartTime: time.Unix(1_700_100_000, 0).UTC(), Duration: 60 * time.Second,
		Timed: true, Complete: true, Valid: true, TrackSlug: "suzuka", TrackConfidence: "confident"})
	const path = "/v1/tracks/suzuka/reference-lap"
	if b := decodeRef(t, doJSON(t, srv, http.MethodPut, path, `{"session_id": "s9", "lap_number": 1}`)); b.Source != "pinned" {
		t.Fatalf("pin in the unknown-total window = %s, want pinned", b.Source)
	}
	_ = sessions.Save(&session.Record{ID: "s9", CarID: 7, TotalLaps: 10, Status: session.StatusActive}) // the Detector persists the total
	if b := decodeRef(t, doJSON(t, srv, http.MethodGet, path, "")); b.Source != "fastest" || b.SessionID != "s1" || b.LapNumber != 2 {
		t.Errorf("GET = %s %s#%d, want fastest s1#2", b.Source, b.SessionID, b.LapNumber)
	}
	if _, ok, _ := laps.PinnedReference("suzuka"); ok {
		t.Error("pin still stored")
	}
}
