package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// fakeTracks is an in-memory rkb.Store for handler tests.
type fakeTracks struct {
	index []rkb.IndexEntry
	byID  map[string]*rkb.Record
}

func (f fakeTracks) Index(context.Context) ([]rkb.IndexEntry, error) { return f.index, nil }
func (f fakeTracks) Track(_ context.Context, slug string) (*rkb.Record, bool, error) {
	r, ok := f.byID[slug]
	return r, ok, nil
}
func (f fakeTracks) ReferenceTime(context.Context, string, string, int, float64) (int, bool, error) {
	return 0, false, nil
}
func (f fakeTracks) Close() error { return nil }

func tracksServer() *Server {
	f := fakeTracks{
		index: []rkb.IndexEntry{{Slug: "hsr", Name: "High Speed Ring", CoordinateUsability: "generated-coarse"}},
		byID:  map[string]*rkb.Record{"hsr": {Slug: "hsr", Name: "High Speed Ring"}},
	}
	return NewServer(coach.New(fakeAnalyzer{}), session.NewMemStore(), nil, nil, nil, f, nil, nil, nil, nil)
}

func TestAPI_Tracks(t *testing.T) {
	srv := tracksServer()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tracks", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var got []rkb.IndexEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Slug != "hsr" {
		t.Errorf("index = %+v", got)
	}
}

func TestAPI_TrackBySlug(t *testing.T) {
	srv := tracksServer()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tracks/hsr", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var got rkb.Record
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Slug != "hsr" {
		t.Errorf("slug = %q", got.Slug)
	}
}

func TestAPI_TrackBySlug_404(t *testing.T) {
	srv := tracksServer()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tracks/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}
