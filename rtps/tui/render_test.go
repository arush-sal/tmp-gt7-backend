package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/coach"
)

func TestRenderSessions(t *testing.T) {
	out := RenderSessions([]SessionSummary{{ID: "s1", CarID: 1384, Status: "finished", CurrentLap: 3, TotalLaps: 2}}, 0)
	for _, want := range []string{"Sessions", "s1", "1384", "finished"} {
		if !strings.Contains(out, want) {
			t.Errorf("session list missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(RenderSessions(nil, 0), "s1") {
		t.Error("empty list should not contain a session")
	}
}

func TestRenderReport(t *testing.T) {
	rep := coach.Report{
		Analysis: analysis.SessionAnalysis{
			SessionID:       "s1",
			HasPB:           true,
			PBLap:           2,
			PBTime:          90 * time.Second,
			TheoreticalBest: 88 * time.Second,
			TimeOnTable:     2 * time.Second,
			Consistency:     analysis.Consistency{Count: 3, Best: 90 * time.Second},
			Laps:            []analysis.LapAnalysis{{LapNumber: 2, Duration: 90 * time.Second}},
			Recommendations: []string{"Personal Best: lap 2 at 1:30.000."},
		},
		Coaching: coach.Coaching{Status: "pending"},
	}
	out := RenderReport(rep)
	for _, want := range []string{"Session s1", "Personal Best", "1:30.000", "Theoretical best", "Recommendations", "pending"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestRenderReport_NoPB(t *testing.T) {
	out := RenderReport(coach.Report{Analysis: analysis.SessionAnalysis{SessionID: "q", HasPB: false}, Coaching: coach.Coaching{Status: "pending"}})
	if !strings.Contains(out, "No valid lap") {
		t.Errorf("no-PB report should say so:\n%s", out)
	}
}
