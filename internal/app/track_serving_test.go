package app

import (
	"context"
	"math"
	"testing"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/rkbref"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// geomProvider serves fixed detection geometry.
type geomProvider []rkb.TrackGeom

func (g geomProvider) DetectionGeometry(context.Context) ([]rkb.TrackGeom, error) { return g, nil }

// askedStore is an rkb.Store that records which track slugs the serving path looked up.
type askedStore struct{ asked []string }

func (s *askedStore) Index(context.Context) ([]rkb.IndexEntry, error) { return nil, nil }
func (s *askedStore) Track(_ context.Context, slug string) (*rkb.Record, bool, error) {
	s.asked = append(s.asked, slug)
	return nil, false, nil
}
func (s *askedStore) ReferenceTime(context.Context, string, string, int, float64) (int, bool, error) {
	return 0, false, nil
}
func (s *askedStore) Close() error { return nil }

func circle(r float64, n int) [][2]float64 {
	pts := make([][2]float64, n)
	for i := range pts {
		th := 2 * math.Pi * float64(i) / float64(n)
		pts[i] = [2]float64{r * math.Cos(th), r * math.Sin(th)}
	}
	return pts
}

// TestServing_NeverConfirmedTrackNotUsedAfterSessionEnd: a session whose detector only ever guessed a
// track (provisional) must not have the sector serving path resolve that guess once it has ended: the
// laps' labels are cleared, so the sectors must fall back (track-not-detected) rather than use a
// possibly wrong layout's geometry.
func TestServing_NeverConfirmedTrackNotUsedAfterSessionEnd(t *testing.T) {
	loop := circle(500, 600)
	enrich := live.NewEnrichmentState()
	// Two layouts of one venue (same Base): no early margin lock, and a short drive cannot reach the
	// coverage a lock needs, so the guess stays provisional.
	det := processing.NewTrackDetector(geomProvider{
		{Slug: "track-a", Base: 7, Points: loop, LengthM: 2 * math.Pi * 500},
		{Slug: "track-a-long", Base: 7, Points: circle(600, 120), LengthM: 2 * math.Pi * 600},
	}, enrich, nil)
	store := &askedStore{}
	kb := rkbref.New(store, enrichSlugFor(enrich), nil)

	rec := &session.Record{ID: "s1"}
	det.OnSessionStart(rec)
	for i, p := range loop[:240] {
		det.OnFrame("s1", &telemetryv1.TelemetryFrame{InRace: true, PositionX: float32(p[0]), PositionZ: float32(p[1]), PackageId: uint32(i)})
	}
	if en := enrich.Get(); en.Track == nil || *en.Track != "track-a" || *en.TrackConfidence != processing.ConfidenceProvisional {
		t.Fatalf("setup: want track-a/provisional published during the session, got %+v", en)
	}

	det.OnSessionEnd(rec)

	_, reason, err := kb.SectorsForSession(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if reason != "track-not-detected" {
		t.Errorf("reason = %q, want track-not-detected", reason)
	}
	if len(store.asked) != 0 {
		t.Errorf("serving path looked up %v for a never-confirmed track, want none", store.asked)
	}
}
