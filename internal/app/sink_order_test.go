package app

import (
	"testing"

	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// lateResolvingDetector stands in for processing.TrackDetector's OnSessionEnd behavior when a match
// only resolves on the session's FINAL attempt (never latched mid-stint): it publishes a Track via
// SetTrack from OnSessionEnd, exactly like the real detector's finishMatch(..., isFinal=true) path.
type lateResolvingDetector struct {
	state *live.EnrichmentState
}

func (d *lateResolvingDetector) OnSessionStart(rec *session.Record) { d.state.ClearTrack(rec.ID) }

func (d *lateResolvingDetector) OnSessionEnd(rec *session.Record) {
	slug, confidence := "watkins-glen-long", "confident"
	d.state.SetTrack(rec.ID, &slug, &confidence)
}

// TestSinkOrder_DetectorBeforeEnrichSink covers the enrichSink/detector interaction in
// internal/app/backend.go's MultiSink wiring.
//
// HISTORY: this test originally guarded a real ordering defect — enrichSink.OnSessionEnd used to do a
// full-snapshot Set(Enrichment{}) that cleared Track, so the detector's OnSessionEnd (which may only
// resolve a match on its FINAL attempt) had to run BEFORE enrichSink's, or its freshly-set Track would
// be wiped again immediately after.
//
// A1 (race-lifecycle: retain enrichment across session end) removed that end-clear: enrichSink.
// OnSessionEnd now only fences its own internal maps (lap/bestLap/active/pending), it no longer touches
// the published EnrichmentState at all. That means the detector-vs-enrichSink ORDER at OnSessionEnd no
// longer affects the outcome — there is nothing left that could race or clobber the detector's SetTrack
// either way. The two subtests below now prove the CURRENT invariants instead:
//  1. Track set by a late-resolving detector at OnSessionEnd is RETAINED after the session ends (post-race
//     analysis keeps it), regardless of registration order.
//  2. The next OnSessionStart for a NEW session id resets Track to nil (no bleed from the previous
//     session) — that protection now lives entirely in OnSessionStart (both detector.ClearTrack and
//     enrichSink's full Set(Enrichment{SessionID: rec.ID})), not in any end-clear.
func TestSinkOrder_DetectorBeforeEnrichSink(t *testing.T) {
	t.Run("either order: Track set at session end is retained (no end-clear anymore)", func(t *testing.T) {
		for _, order := range []string{"detector then enrichSink", "enrichSink then detector"} {
			t.Run(order, func(t *testing.T) {
				rec := &session.Record{ID: "sess-1"}
				state := live.NewEnrichmentState()
				enrichSink := live.NewEnrichSink(analysis.Analyzer{}, state, nil)
				defer enrichSink.Close()
				detector := &lateResolvingDetector{state: state}

				detector.OnSessionStart(rec)
				enrichSink.OnSessionStart(rec)

				if order == "detector then enrichSink" {
					detector.OnSessionEnd(rec)
					enrichSink.OnSessionEnd(rec)
				} else {
					enrichSink.OnSessionEnd(rec)
					detector.OnSessionEnd(rec)
				}

				got := state.Get()
				if got.Track == nil {
					t.Fatal("expected the late-resolving Track to be RETAINED after session end, got nil")
				}
			})
		}
	})

	t.Run("next OnSessionStart resets Track for the new session (no stale bleed)", func(t *testing.T) {
		state := live.NewEnrichmentState()
		enrichSink := live.NewEnrichSink(analysis.Analyzer{}, state, nil)
		defer enrichSink.Close()
		detector := &lateResolvingDetector{state: state}

		rec1 := &session.Record{ID: "sess-1"}
		detector.OnSessionStart(rec1)
		enrichSink.OnSessionStart(rec1)
		detector.OnSessionEnd(rec1)
		enrichSink.OnSessionEnd(rec1)

		if got := state.Get(); got.Track == nil {
			t.Fatal("precondition failed: expected Track retained from session 1 before starting session 2")
		}

		rec2 := &session.Record{ID: "sess-2"}
		detector.OnSessionStart(rec2)
		enrichSink.OnSessionStart(rec2)

		got := state.Get()
		if got.Track != nil {
			t.Fatalf("expected Track reset to nil for new session sess-2, got %q", *got.Track)
		}
		if got.SessionID != rec2.ID {
			t.Fatalf("expected SessionID = %q for new session, got %q", rec2.ID, got.SessionID)
		}
	})
}
