package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// f64 builds a nullable RKB coordinate literal.
func f64(v float64) *float64 { return &v }

func TestAPI_TracksCoverage_RKBNil(t *testing.T) {
	srv := NewServer(coach.New(fakeAnalyzer{}), session.NewMemStore(), nil, nil, nil, nil, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tracks/coverage", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestAPI_TracksCoverage(t *testing.T) {
	// "ready": a 3-vertex centerline + 2 finite sector labels -> usable geometry.
	ready := &rkb.Record{
		Slug: "ready", Name: "Ready Circuit",
		Boundary: rkb.Boundary{Cl: [][]*float64{{f64(0), f64(0)}, {f64(100), f64(0)}, {f64(200), f64(0)}}},
		Sectors: []rkb.Sector{
			{Sector: "S1", LabelWorld: &[]float64{50, 0}},
			{Sector: "S2", LabelWorld: &[]float64{150, 0}},
		},
	}
	// "nocl": no centerline -> no-centerline reason.
	nocl := &rkb.Record{Slug: "nocl", Name: "No Centerline"}

	f := fakeTracks{
		index: []rkb.IndexEntry{
			{Slug: "ready", Name: "Ready Circuit"},
			{Slug: "nocl", Name: "No Centerline", NeedsCapture: true},
		},
		byID: map[string]*rkb.Record{"ready": ready, "nocl": nocl},
	}
	srv := NewServer(coach.New(fakeAnalyzer{}), session.NewMemStore(), nil, nil, nil, f, nil, nil, nil, nil)

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/tracks/coverage", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var got []trackCoverageDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("coverage = %d entries, want 2", len(got))
	}

	r, n := got[0], got[1]
	if r.Slug != "ready" || !r.HasCenterline || !r.SectorReady || r.SectorLabelCount != 2 {
		t.Errorf("ready = %+v, want has_cl+ready+2 labels", r)
	}
	if n.Slug != "nocl" || n.HasCenterline || n.SectorReady || !n.NeedsCapture {
		t.Errorf("nocl = %+v, want no-cl/not-ready/needs-capture", n)
	}
}
