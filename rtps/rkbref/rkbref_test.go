package rkbref

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/arush-sal/garage/backend/rtps/rkb"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// fakeStore is an in-memory rkb.Store keyed by slug. Only Track is exercised by the adapter.
type fakeStore struct{ recs map[string]*rkb.Record }

func (f fakeStore) Index(context.Context) ([]rkb.IndexEntry, error) { return nil, nil }
func (f fakeStore) Track(_ context.Context, slug string) (*rkb.Record, bool, error) {
	r, ok := f.recs[slug]
	return r, ok, nil
}
func (f fakeStore) ReferenceTime(context.Context, string, string, int, float64) (int, bool, error) {
	return 0, false, nil
}
func (f fakeStore) Close() error { return nil }

func fptr(v float64) *float64 { return &v }

// clPts builds a centerline as [][]*float64. A NaN coord marks a null (nil) vertex coord.
func clPts(pts ...[2]float64) [][]*float64 {
	out := make([][]*float64, len(pts))
	for i, p := range pts {
		var x, z *float64
		if !math.IsNaN(p[0]) {
			x = fptr(p[0])
		}
		if !math.IsNaN(p[1]) {
			z = fptr(p[1])
		}
		out[i] = []*float64{x, z}
	}
	return out
}

func label(name string, x, z float64) rkb.Sector {
	lw := []float64{x, z}
	return rkb.Sector{Sector: name, LabelWorld: &lw}
}

func rec(cl [][]*float64, sectors ...rkb.Sector) *rkb.Record {
	return &rkb.Record{
		Slug:     "t",
		Name:     "Test Track",
		LengthM:  fptr(30),
		Boundary: rkb.Boundary{Source: "victorydash", Cl: cl},
		Sectors:  sectors,
	}
}

func TestSectorsFromRecord(t *testing.T) {
	denseCl := clPts([2]float64{0, 0}, [2]float64{10, 0}, [2]float64{20, 0}, [2]float64{30, 0})

	tests := []struct {
		name       string
		rec        *rkb.Record
		wantReason string
		wantFracs  []float64 // End of each emitted sector; nil when reason != ""
	}{
		{
			name:       "happy 3 labels",
			rec:        rec(denseCl, label("1", 5, 0), label("2", 15, 0), label("3", 25, 0)),
			wantReason: "",
			wantFracs:  []float64{10.0 / 30, 20.0 / 30, 1},
		},
		{
			name:       "happy 2 labels",
			rec:        rec(denseCl, label("1", 5, 0), label("2", 25, 0)),
			wantReason: "",
			wantFracs:  []float64{0.5, 1},
		},
		{
			name:       "labels out of order sort ascending",
			rec:        rec(denseCl, label("2", 25, 0), label("1", 5, 0)),
			wantReason: "",
			wantFracs:  []float64{0.5, 1},
		},
		{
			name:       "null label coords skipped -> too few",
			rec:        rec(denseCl, label("1", 5, 0), rkb.Sector{Sector: "2", LabelWorld: nil}),
			wantReason: "no-sector-labels",
		},
		{
			name:       "null cl vertex skipped still works",
			rec:        rec(clPts([2]float64{0, 0}, [2]float64{math.NaN(), math.NaN()}, [2]float64{10, 0}, [2]float64{20, 0}, [2]float64{30, 0}), label("1", 5, 0), label("2", 25, 0)),
			wantReason: "",
			wantFracs:  []float64{0.5, 1},
		},
		{
			name:       "empty cl",
			rec:        rec(nil, label("1", 5, 0), label("2", 25, 0)),
			wantReason: "no-centerline",
		},
		{
			name:       "one-point cl",
			rec:        rec(clPts([2]float64{0, 0}), label("1", 5, 0), label("2", 25, 0)),
			wantReason: "no-centerline",
		},
		{
			name:       "fewer than 2 labels",
			rec:        rec(denseCl, label("1", 5, 0)),
			wantReason: "no-sector-labels",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			secs, reason := SectorsFromRecord(tc.rec)
			if reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReason)
			}
			if tc.wantReason != "" {
				if secs != nil {
					t.Fatalf("sectors = %v, want nil when reason set", secs)
				}
				return
			}
			if len(secs) != len(tc.wantFracs) {
				t.Fatalf("got %d sectors, want %d", len(secs), len(tc.wantFracs))
			}
			// Contiguous, spanning [0,1], monotonic, correct Index.
			prev := 0.0
			for i, s := range secs {
				if s.Index != i {
					t.Errorf("sector[%d].Index = %d", i, s.Index)
				}
				if math.Abs(s.Start-prev) > 1e-9 {
					t.Errorf("sector[%d].Start = %v, want %v (contiguous)", i, s.Start, prev)
				}
				if math.Abs(s.End-tc.wantFracs[i]) > 1e-9 {
					t.Errorf("sector[%d].End = %v, want %v", i, s.End, tc.wantFracs[i])
				}
				if s.End < s.Start {
					t.Errorf("sector[%d] not monotonic: %v..%v", i, s.Start, s.End)
				}
				prev = s.End
			}
			if math.Abs(secs[0].Start) > 1e-9 {
				t.Errorf("first sector Start = %v, want 0", secs[0].Start)
			}
			if math.Abs(secs[len(secs)-1].End-1) > 1e-9 {
				t.Errorf("last sector End = %v, want 1", secs[len(secs)-1].End)
			}
		})
	}
}

func TestTrackForSession(t *testing.T) {
	store := fakeStore{recs: map[string]*rkb.Record{
		"spa": {Slug: "spa", Name: "Spa", LengthM: fptr(7004)},
	}}
	slugs := map[string]string{"s1": "spa", "s2": "unknown-slug"}
	k := New(store, func(id string) (string, bool) {
		s, ok := slugs[id]
		return s, ok
	}, zap.NewNop())
	ctx := context.Background()

	tr, found, err := k.TrackForSession(ctx, "s1")
	if err != nil || !found {
		t.Fatalf("s1: found=%v err=%v", found, err)
	}
	if tr.ID != "spa" || tr.Name != "Spa" || tr.LengthM != 7004 {
		t.Errorf("s1 track = %+v", tr)
	}

	if _, found, _ := k.TrackForSession(ctx, "s2"); found {
		t.Error("s2: slug resolves but no record -> want found=false")
	}
	if _, found, _ := k.TrackForSession(ctx, "sX"); found {
		t.Error("sX: no slug -> want found=false")
	}
}

func TestSectorsForSession(t *testing.T) {
	denseCl := clPts([2]float64{0, 0}, [2]float64{10, 0}, [2]float64{20, 0}, [2]float64{30, 0})
	good := rec(denseCl, label("1", 5, 0), label("2", 25, 0))
	good.Slug = "good"
	noCl := rec(nil, label("1", 5, 0), label("2", 25, 0))
	noCl.Slug = "nocl"

	store := fakeStore{recs: map[string]*rkb.Record{"good": good, "nocl": noCl}}
	slugs := map[string]string{"sg": "good", "sn": "nocl"}

	core, logs := observer.New(zap.WarnLevel)
	k := New(store, func(id string) (string, bool) {
		s, ok := slugs[id]
		return s, ok
	}, zap.New(core))
	ctx := context.Background()

	// No slug detected.
	if secs, reason, err := k.SectorsForSession(ctx, "none"); secs != nil || reason != "track-not-detected" || err != nil {
		t.Errorf("undetected = (%v,%q,%v)", secs, reason, err)
	}

	// Good record -> real sectors, no reason, no log.
	secs, reason, err := k.SectorsForSession(ctx, "sg")
	if err != nil || reason != "" || len(secs) != 2 {
		t.Fatalf("good = (%d sectors,%q,%v)", len(secs), reason, err)
	}

	// No-centerline detected slug: reason surfaced, warn logged.
	if secs, reason, _ := k.SectorsForSession(ctx, "sn"); secs != nil || reason != "no-centerline" {
		t.Fatalf("nocl = (%v,%q)", secs, reason)
	}
	// Repeat same slug several times: still exactly ONE warn.
	for i := 0; i < 4; i++ {
		_, _, _ = k.SectorsForSession(ctx, "sn")
	}
	warns := logs.FilterMessage("RKB geometry missing for sector timing").All()
	if len(warns) != 1 {
		t.Fatalf("warn count = %d, want 1 (warn-once-per-slug)", len(warns))
	}
	f := warns[0].ContextMap()
	if f["slug"] != "nocl" || f["reason"] != "no-centerline" {
		t.Errorf("warn fields = %v", f)
	}
}

// TestSectorsForTrack: the per-lap path resolves a slug directly (no session lookup), with the same
// reasons as SectorsForSession.
func TestSectorsForTrack(t *testing.T) {
	denseCl := clPts([2]float64{0, 0}, [2]float64{10, 0}, [2]float64{20, 0}, [2]float64{30, 0})
	store := fakeStore{recs: map[string]*rkb.Record{
		"good": rec(denseCl, label("1", 5, 0), label("2", 25, 0)),
		"nocl": rec(nil, label("1", 5, 0), label("2", 25, 0)),
	}}
	k := New(store, func(string) (string, bool) { return "", false }, nil)
	ctx := context.Background()
	if secs, reason, err := k.SectorsForTrack(ctx, "good"); err != nil || reason != "" || len(secs) != 2 {
		t.Errorf("good = (%d,%q,%v)", len(secs), reason, err)
	}
	if secs, reason, _ := k.SectorsForTrack(ctx, "nocl"); secs != nil || reason != "no-centerline" {
		t.Errorf("nocl = (%v,%q)", secs, reason)
	}
	if secs, reason, _ := k.SectorsForTrack(ctx, "unknown"); secs != nil || reason != "track-not-detected" {
		t.Errorf("unknown = (%v,%q)", secs, reason)
	}
}

// TestSectorsFromRecord_RealDB is a best-effort integration check against ~/.garage/rkb.db.
// Skipped (not failed) when the file is absent or Open fails, so the suite stays green off-box.
func TestSectorsFromRecord_RealDB(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	path := filepath.Join(home, ".garage", "rkb.db")
	if _, err := os.Stat(path); err != nil {
		t.Skip("no rkb.db")
	}
	store, err := rkb.Open(path)
	if err != nil {
		t.Skipf("rkb.Open: %v", err)
	}
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	idx, err := store.Index(ctx)
	if err != nil {
		t.Skipf("Index: %v", err)
	}
	found := false
	for _, e := range idx {
		r, ok, err := store.Track(ctx, e.Slug)
		if err != nil || !ok {
			continue
		}
		if secs, reason := SectorsFromRecord(r); reason == "" && len(secs) >= 1 {
			found = true
			// Sanity: contiguous span [0,1].
			if math.Abs(secs[0].Start) > 1e-6 || math.Abs(secs[len(secs)-1].End-1) > 1e-6 {
				t.Errorf("%s: not spanning [0,1]: %+v", e.Slug, secs)
			}
			break
		}
	}
	if !found {
		t.Skip("no measured (centerline) track produced sectors in this rkb.db")
	}
}
