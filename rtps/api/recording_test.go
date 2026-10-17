package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/session"
)

// fakeSwitch is a RecordingSwitch with a scripted session: active decides ErrNoSession, cur is the open
// session (an off request naming another id is ErrSessionChanged), stalled forces ErrStalled.
type fakeSwitch struct {
	active    bool
	stalled   bool
	recording bool
	id, cur   string
	gotExpect string
}

func (f *fakeSwitch) State() (bool, string) { return f.recording, f.id }

func (f *fakeSwitch) Set(on bool, expect string, _ time.Duration) (bool, string, error) {
	f.gotExpect = expect
	switch {
	case !f.active:
		return f.recording, "", session.ErrNoSession
	case !on && expect != "" && expect != f.cur:
		return f.recording, "", session.ErrSessionChanged
	case f.stalled:
		return f.recording, "", session.ErrStalled
	}
	f.recording = on
	if on {
		f.id, f.cur = "fresh", "fresh"
	} else {
		f.id = ""
	}
	return f.recording, f.id, nil
}

func postRecording(srv *Server, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/live/recording", strings.NewReader(body))
	req.Host = "localhost:8080" // mutating routes only accept loopback / allow-listed hosts
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

// TestAPI_Recording covers the contract: 200 with {recording, session_id}, the optional session_id
// target (409 "session changed" on mismatch), 409 when no session is open, 503 when telemetry stalled,
// 400 on a malformed body, idempotency, and the live snapshot's enrichment.recording.
func TestAPI_Recording(t *testing.T) {
	srv, _ := testServer()
	sw := &fakeSwitch{active: true, recording: true, id: "s1", cur: "s1"}
	srv.Recording = sw

	rr := postRecording(srv, `{"recording": false, "session_id": "other"}`)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), `"error":"session changed"`) {
		t.Fatalf("off for another session: %d %s, want 409 session changed", rr.Code, rr.Body)
	}
	if !sw.recording {
		t.Fatal("mismatched off request changed state")
	}

	rr = postRecording(srv, `{"recording": false, "session_id": "s1"}`)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"recording":false,"session_id":""}` || sw.gotExpect != "s1" {
		t.Fatalf("off: %d %s (expect passed %q)", rr.Code, rr.Body, sw.gotExpect)
	}
	live := httptest.NewRecorder()
	srv.Handler().ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/v1/live", nil))
	var snap struct {
		Enrichment map[string]json.RawMessage `json:"enrichment"`
	}
	if err := json.Unmarshal(live.Body.Bytes(), &snap); err != nil || string(snap.Enrichment["recording"]) != "false" {
		t.Errorf("live enrichment.recording = %s (err %v), want false", snap.Enrichment["recording"], err)
	}

	rr = postRecording(srv, `{"recording": true}`) // session_id optional
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"recording":true,"session_id":"fresh"}` {
		t.Fatalf("on: %d %s", rr.Code, rr.Body)
	}
	if rr := postRecording(srv, `{"recording": true}`); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"recording":true`) {
		t.Errorf("idempotent on: %d %s, want 200 recording true", rr.Code, rr.Body)
	}

	for _, body := range []string{``, `{`, `{"recording": "no"}`, `{}`, `{"recording": null}`, `{"recording": false, "session_id": 7}`} {
		if rr := postRecording(srv, body); rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: status %d, want 400", body, rr.Code)
		}
	}

	sw.stalled = true
	rr = postRecording(srv, `{"recording": false}`)
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), `"error":"telemetry stalled; recording unchanged"`) {
		t.Errorf("stalled: %d %s, want 503 telemetry stalled", rr.Code, rr.Body)
	}
	sw.stalled = false

	sw.active = false
	rr = postRecording(srv, `{"recording": false}`)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), `"error"`) {
		t.Errorf("idle: %d %s, want 409 with error JSON", rr.Code, rr.Body)
	}
}

// TestAPI_LiveRecordingDefaultTrue: with no switch wired (or a fresh one) enrichment.recording is true.
func TestAPI_LiveRecordingDefaultTrue(t *testing.T) {
	srv, _ := testServer()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/live", nil))
	if !strings.Contains(rr.Body.String(), `"recording":true`) {
		t.Errorf("live = %s, want enrichment.recording true", rr.Body)
	}
	if rr := postRecording(srv, `{"recording": false}`); rr.Code != http.StatusConflict {
		t.Errorf("no switch wired: status %d, want 409", rr.Code)
	}
}
