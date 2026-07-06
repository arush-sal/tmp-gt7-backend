// Package coach is the Coaching domain (GARAGE domain 5): it converts Analysis findings into coaching
// output — mistakes, priorities, drills.
//
// FOR NOW it is a placeholder passthrough (D-B21): it forwards the deterministic
// analysis.SessionAnalysis to the API and marks the coaching layer `pending`. The real coach (per-
// corner deltas vs a theoretical-best reference -> prioritized, drillable advice) lands once there is
// enough multi-lap data to build a meaningful reference. Coaching NEVER analyzes telemetry itself
// (handoff principle) — it only consumes Analysis output.
package coach

import (
	"context"
	"time"

	"github.com/arush-sal/garage/backend/rtps/analysis"
)

// Coaching is the coaching layer of a report. Placeholder for now; the fields for mistakes/priorities/
// drills arrive with the real coach.
type Coaching struct {
	Status string `json:"status"` // "pending" until the coach analysis is implemented
	Note   string `json:"note"`
}

// Report is what the Coaching domain hands to the API: the deterministic analysis plus the coaching
// layer (currently a pending placeholder).
type Report struct {
	Analysis analysis.SessionAnalysis `json:"analysis"`
	Coaching Coaching                 `json:"coaching"`
}

// analyzer is the slice of Analysis the coach needs (an interface so tests can inject a fake).
type analyzer interface {
	AnalyzeSession(ctx context.Context, sessionID string, gt7Best time.Duration) (analysis.SessionAnalysis, error)
}

// Coach produces coaching Reports from analysis.
type Coach struct {
	analyzer analyzer
}

// New wires a Coach to an analyzer (analysis.Analyzer satisfies the interface).
func New(a analyzer) Coach { return Coach{analyzer: a} }

// pendingNote explains the placeholder state in the response itself.
const pendingNote = "Coaching layer not yet implemented; deterministic analysis is passed through. " +
	"Corner-level, improvement-oriented coaching is TBD pending sufficient data (see D-B21)."

// SessionReport runs the analysis for a session against the GT7-authoritative best lap time and wraps
// it in a coaching Report.
func (c Coach) SessionReport(ctx context.Context, sessionID string, gt7Best time.Duration) (Report, error) {
	a, err := c.analyzer.AnalyzeSession(ctx, sessionID, gt7Best)
	if err != nil {
		return Report{}, err
	}
	return Report{
		Analysis: a,
		Coaching: Coaching{Status: "pending", Note: pendingNote},
	}, nil
}
