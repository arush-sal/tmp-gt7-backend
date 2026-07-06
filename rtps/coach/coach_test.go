package coach

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/analysis"
)

type fakeAnalyzer struct {
	ret analysis.SessionAnalysis
	err error
}

func (f fakeAnalyzer) AnalyzeSession(context.Context, string, time.Duration) (analysis.SessionAnalysis, error) {
	return f.ret, f.err
}

// TestSessionReport_Passthrough: the report carries the analysis unchanged and tags coaching pending.
func TestSessionReport_Passthrough(t *testing.T) {
	a := analysis.SessionAnalysis{SessionID: "s1", HasPB: true, PBLap: 2, Recommendations: []string{"PB: lap 2"}}
	rep, err := New(fakeAnalyzer{ret: a}).SessionReport(context.Background(), "s1", 90*time.Second)
	if err != nil {
		t.Fatalf("SessionReport: %v", err)
	}
	if rep.Analysis.SessionID != "s1" || rep.Analysis.PBLap != 2 || len(rep.Analysis.Recommendations) != 1 {
		t.Errorf("analysis not passed through: %+v", rep.Analysis)
	}
	if rep.Coaching.Status != "pending" || rep.Coaching.Note == "" {
		t.Errorf("coaching placeholder = %+v, want pending + note", rep.Coaching)
	}
}

// TestSessionReport_Error: an analysis error propagates.
func TestSessionReport_Error(t *testing.T) {
	_, err := New(fakeAnalyzer{err: errors.New("boom")}).SessionReport(context.Background(), "s1", 90*time.Second)
	if err == nil {
		t.Error("want error from failing analyzer, got nil")
	}
}
