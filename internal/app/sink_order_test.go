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

// TestSinkOrder_DetectorBeforeEnrichSink is the regression test for the cross-commit ordering defect:
// on OnSessionEnd, a detector that only resolves a track on its FINAL match attempt (per
// processing.TrackDetector.OnSessionEnd/finishMatch) must not leave a stale Track in the shared
// EnrichmentState after the session has ended. That requires the detector's OnSessionEnd to run
// BEFORE enrichSink's, so enrichSink's full-snapshot clear (Set{}) is the last write and wins.
//
// This test drives both possible MultiSink registration orders directly (old buggy order vs. the
// fixed order in internal/app/backend.go) against the SAME lateResolvingDetector + a real
// live.EnrichSink, proving the fix is the ordering itself, not any detector/enrichSink internal.
func TestSinkOrder_DetectorBeforeEnrichSink(t *testing.T) {
	rec := &session.Record{ID: "sess-1"}

	t.Run("old order (enrichSink before detector) leaves a stale Track: bug", func(t *testing.T) {
		state := live.NewEnrichmentState()
		enrichSink := live.NewEnrichSink(analysis.Analyzer{}, state, nil)
		defer enrichSink.Close()
		detector := &lateResolvingDetector{state: state}

		// Old (buggy) order: enrichSink.OnSessionEnd (clears) runs, THEN detector.OnSessionEnd
		// (re-populates Track) runs.
		enrichSink.OnSessionStart(rec)
		detector.OnSessionStart(rec)
		enrichSink.OnSessionEnd(rec)
		detector.OnSessionEnd(rec)

		got := state.Get()
		if got.Track == nil {
			t.Fatal("expected the (intentionally reproduced) bug: stale Track present after session end, got nil")
		}
	})

	t.Run("new order (detector before enrichSink) clears Track: fixed", func(t *testing.T) {
		state := live.NewEnrichmentState()
		enrichSink := live.NewEnrichSink(analysis.Analyzer{}, state, nil)
		defer enrichSink.Close()
		detector := &lateResolvingDetector{state: state}

		// Fixed order (matches internal/app/backend.go's MultiSink wiring): detector.OnSessionEnd
		// (may SetTrack) runs first, THEN enrichSink.OnSessionEnd's full Set{} clears everything, so
		// the snapshot is clean between stints.
		detector.OnSessionStart(rec)
		enrichSink.OnSessionStart(rec)
		detector.OnSessionEnd(rec)
		enrichSink.OnSessionEnd(rec)

		got := state.Get()
		if got.Track != nil {
			t.Fatalf("expected Track cleared after session end, got %q", *got.Track)
		}
	})
}
