package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arush-sal/garage/backend/rtps/rkb"
)

// fakePicker is a TrackPicker with a scripted open session (cur "" = idle).
type fakePicker struct {
	cur, pending, user string
	applied            string
	cleared            int
}

func (f *fakePicker) Set(slug string) (string, string) {
	if f.cur == "" {
		f.pending = slug
		return "next", ""
	}
	f.applied = slug
	return "current", f.cur
}
func (f *fakePicker) Clear()            { f.pending = ""; f.cleared++ }
func (f *fakePicker) Pending() string   { return f.pending }
func (f *fakePicker) UserTrack() string { return f.user }

func liveTrackServer() (*Server, *fakePicker) {
	srv, _ := testServer()
	srv.tracks = fakeTracks{byID: map[string]*rkb.Record{"suzuka": {Slug: "suzuka"}, "red-bull-ring": {Slug: "red-bull-ring"}}}
	p := &fakePicker{}
	srv.TrackPick = p
	return srv, p
}

func liveTrackReq(srv *Server, method, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/v1/live/track", strings.NewReader(body))
	req.Host = "localhost:8080"
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

// TestAPI_LiveTrack covers POST/DELETE /v1/live/track: 200 current/next, 404 unknown track, 400 bad
// or missing body, and DELETE's 200 {"track_slug": null}.
func TestAPI_LiveTrack(t *testing.T) {
	srv, p := liveTrackServer()

	rr := liveTrackReq(srv, http.MethodPost, `{"track_slug": "suzuka"}`)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"track_slug":"suzuka","applies_to":"next","session_id":""}` {
		t.Fatalf("idle pick: %d %s", rr.Code, rr.Body)
	}
	if p.pending != "suzuka" {
		t.Fatalf("pending = %q", p.pending)
	}

	p.cur = "s1"
	rr = liveTrackReq(srv, http.MethodPost, `{"track_slug": "red-bull-ring"}`)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"track_slug":"red-bull-ring","applies_to":"current","session_id":"s1"}` {
		t.Fatalf("live pick: %d %s", rr.Code, rr.Body)
	}

	rr = liveTrackReq(srv, http.MethodPost, `{"track_slug": "nowhere"}`)
	if rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), `"error":"unknown track"`) {
		t.Fatalf("unknown slug: %d %s, want 404 unknown track", rr.Code, rr.Body)
	}
	if p.applied != "red-bull-ring" {
		t.Fatalf("an unknown slug reached the picker: %q", p.applied)
	}

	for _, body := range []string{``, `{`, `{}`, `{"track_slug": null}`, `{"track_slug": ""}`, `{"track_slug": 3}`} {
		if rr := liveTrackReq(srv, http.MethodPost, body); rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: %d %s, want 400", body, rr.Code, rr.Body)
		}
	}

	rr = liveTrackReq(srv, http.MethodDelete, ``)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"track_slug":null}` || p.cleared != 1 {
		t.Fatalf("delete: %d %s cleared=%d", rr.Code, rr.Body, p.cleared)
	}
}

// TestAPI_LiveTrack_Enrichment: the live snapshot carries track_source (user/detected/null) and
// pending_track_slug, leaving the existing track fields as published.
func TestAPI_LiveTrack_Enrichment(t *testing.T) {
	srv, p := liveTrackServer()
	enrichment := func() map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/live", nil))
		var body struct {
			Enrichment map[string]any `json:"enrichment"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body.Enrichment
	}

	en := enrichment()
	if v, ok := en["track_source"]; !ok || v != nil {
		t.Fatalf("no track: track_source = %v (present %v), want null", v, ok)
	}
	if v, ok := en["pending_track_slug"]; !ok || v != nil {
		t.Fatalf("no pick: pending_track_slug = %v (present %v), want null", v, ok)
	}

	p.pending = "suzuka"
	slug, conf := "track-a", "confident"
	srv.enrich.SetTrack("s1", &slug, &conf)
	en = enrichment()
	if en["track_source"] != "detected" || en["pending_track_slug"] != "suzuka" || en["track"] != "track-a" {
		t.Fatalf("detected + pending: %v", en)
	}

	p.pending, p.user = "", "suzuka"
	slug = "suzuka"
	srv.enrich.SetTrack("s1", &slug, &conf)
	en = enrichment()
	if en["track_source"] != "user" || en["track"] != "suzuka" || en["track_confidence"] != "confident" || en["pending_track_slug"] != nil {
		t.Fatalf("user pick: %v", en)
	}

	// Without a picker wired, a published track is detected.
	srv.TrackPick = nil
	if en = enrichment(); en["track_source"] != "detected" {
		t.Fatalf("no picker: %v", en)
	}
}
