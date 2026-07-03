package processing

import (
	"errors"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var segBase = time.Unix(1_700_000_000, 0)

// mkFrame builds a frame at segBase+ms with a current_lap, an optional last_lap (ms; set on the first
// frame of a new lap, mirroring GT7 reporting the just-completed lap), and a gap flag.
func mkFrame(ms int, curLap int32, lastLapMs int, gap bool) *telemetryv1.TelemetryFrame {
	f := &telemetryv1.TelemetryFrame{
		RecvTimestamp: timestamppb.New(segBase.Add(time.Duration(ms) * time.Millisecond)),
		CurrentLap:    curLap,
		Gap:           gap,
	}
	if lastLapMs > 0 {
		f.LastLap = durationpb.New(time.Duration(lastLapMs) * time.Millisecond)
	}
	return f
}

// runSeg drives a segmenter through one session's frames and returns that session's laps.
func runSeg(frames ...*telemetryv1.TelemetryFrame) []*Lap {
	store := NewMemLapStore()
	seg := NewLapSegmenter(store)
	rec := &session.Record{ID: "s1"}
	seg.OnSessionStart(rec)
	for _, f := range frames {
		seg.OnFrame("s1", f)
	}
	seg.OnSessionEnd(rec)
	return store.BySession("s1")
}

// TestSegmenter_ThreeLaps: formation lap 0, timed laps 1 and 2, with the session ending mid-lap-2.
// Lap 0 is untimed; lap 1 is a complete valid timed lap; lap 2 is the incomplete final lap.
func TestSegmenter_ThreeLaps(t *testing.T) {
	laps := runSeg(
		mkFrame(0, 0, 0, false),
		mkFrame(100, 0, 0, false),
		mkFrame(200, 1, 30_000, false), // 0->1: closes lap 0 with last_lap=30s
		mkFrame(300, 1, 0, false),
		mkFrame(400, 2, 90_000, false), // 1->2: closes lap 1 with last_lap=90s
		mkFrame(500, 2, 0, false),
	)
	if len(laps) != 3 {
		t.Fatalf("got %d laps, want 3", len(laps))
	}

	l0, l1, l2 := laps[0], laps[1], laps[2]
	if l0.LapNumber != 0 || l0.Timed || l0.Valid {
		t.Errorf("lap0 = %+v, want number 0, untimed, invalid", l0)
	}
	if l0.Duration != 30*time.Second {
		t.Errorf("lap0 duration = %s, want 30s (last_lap at 0->1)", l0.Duration)
	}
	if l1.LapNumber != 1 || !l1.Timed || !l1.Complete || !l1.Valid {
		t.Errorf("lap1 = %+v, want timed complete valid", l1)
	}
	if l1.Duration != 90*time.Second {
		t.Errorf("lap1 duration = %s, want 90s (last_lap at 1->2)", l1.Duration)
	}
	if l2.LapNumber != 2 || l2.Complete || l2.Valid {
		t.Errorf("lap2 = %+v, want number 2, incomplete, invalid (session ended mid-lap)", l2)
	}
	// The final lap has no authoritative last_lap, so its duration is the wall-clock span.
	if l2.Duration != 100*time.Millisecond {
		t.Errorf("lap2 duration = %s, want 100ms (fallback span)", l2.Duration)
	}
}

// TestSegmenter_GapInvalidatesLap: a gapped frame in an otherwise complete timed lap marks it invalid
// (the trace is unreliable) while still recording it.
func TestSegmenter_GapInvalidatesLap(t *testing.T) {
	laps := runSeg(
		mkFrame(0, 1, 0, false),
		mkFrame(100, 1, 0, true), // gap inside lap 1
		mkFrame(200, 2, 60_000, false),
		mkFrame(300, 2, 0, false),
	)
	// laps[0] is lap 1 (complete, but gapped)
	if laps[0].LapNumber != 1 || !laps[0].Complete {
		t.Fatalf("laps[0] = %+v, want complete lap 1", laps[0])
	}
	if laps[0].GapCount != 1 {
		t.Errorf("lap1 GapCount = %d, want 1", laps[0].GapCount)
	}
	if laps[0].Valid {
		t.Error("lap1 Valid = true, want false (gapped)")
	}
}

// TestSegmenter_FrameCount: frames are attributed to the correct lap (the increment frame counts to
// the new lap, not the closed one).
func TestSegmenter_FrameCount(t *testing.T) {
	laps := runSeg(
		mkFrame(0, 1, 0, false),
		mkFrame(100, 1, 0, false),
		mkFrame(200, 1, 0, false), // lap 1 = 3 frames
		mkFrame(300, 2, 60_000, false),
		mkFrame(400, 2, 0, false), // lap 2 = 2 frames (so far)
	)
	if laps[0].FrameCount != 3 {
		t.Errorf("lap1 FrameCount = %d, want 3", laps[0].FrameCount)
	}
	if laps[1].FrameCount != 2 {
		t.Errorf("lap2 FrameCount = %d, want 2", laps[1].FrameCount)
	}
}

// TestSegmenter_NoFramesNoLaps: a session that starts and ends with no in-race frames emits nothing.
func TestSegmenter_NoFramesNoLaps(t *testing.T) {
	if laps := runSeg(); len(laps) != 0 {
		t.Errorf("got %d laps, want 0", len(laps))
	}
}

// TestSegmenter_DecreasingLapIgnored: a spurious drop in current_lap must NOT close a lap or create a
// duplicate/out-of-order lap number — only an increase is a boundary.
func TestSegmenter_DecreasingLapIgnored(t *testing.T) {
	laps := runSeg(
		mkFrame(0, 1, 0, false),
		mkFrame(100, 1, 0, false),
		mkFrame(200, 2, 60_000, false), // 1->2 boundary (the only real one)
		mkFrame(300, 1, 0, false),      // glitch: current_lap drops — must be ignored
		mkFrame(400, 2, 0, false),
	)
	if len(laps) != 2 {
		t.Fatalf("got %d laps %v, want 2 (decrease must not split)", len(laps), lapNums(laps))
	}
	if laps[0].LapNumber != 1 || laps[1].LapNumber != 2 {
		t.Errorf("lap numbers = %v, want [1 2]", lapNums(laps))
	}
}

// TestSegmenter_FrameAfterEndDropped: a stray frame arriving after OnSessionEnd must not resurrect
// session state or emit a bogus lap.
func TestSegmenter_FrameAfterEndDropped(t *testing.T) {
	store := NewMemLapStore()
	seg := NewLapSegmenter(store)
	rec := &session.Record{ID: "s1"}
	seg.OnSessionStart(rec)
	seg.OnFrame("s1", mkFrame(0, 1, 0, false))
	seg.OnFrame("s1", mkFrame(100, 1, 0, false))
	seg.OnSessionEnd(rec)                        // flushes lap 1
	seg.OnFrame("s1", mkFrame(200, 2, 0, false)) // stray post-end frame

	if laps := store.BySession("s1"); len(laps) != 1 {
		t.Errorf("got %d laps, want 1 (post-end frame must be dropped)", len(laps))
	}
}

func lapNums(laps []*Lap) []int32 {
	n := make([]int32, len(laps))
	for i, l := range laps {
		n[i] = l.LapNumber
	}
	return n
}

// errLapStore fails every Save — models a DB outage.
type errLapStore struct{ saves int }

func (e *errLapStore) Save(*Lap) error         { e.saves++; return errors.New("boom") }
func (e *errLapStore) BySession(string) []*Lap { return nil }
func (e *errLapStore) List() []*Lap            { return nil }

// TestSegmenter_SaveErrorSurfaced: a lap-store failure must reach OnError rather than being swallowed.
func TestSegmenter_SaveErrorSurfaced(t *testing.T) {
	var errs int
	seg := NewLapSegmenter(&errLapStore{})
	seg.OnError = func(error) { errs++ }
	rec := &session.Record{ID: "s1"}
	seg.OnSessionStart(rec)
	seg.OnFrame("s1", mkFrame(0, 1, 0, false))
	seg.OnFrame("s1", mkFrame(100, 2, 60_000, false)) // boundary -> emit lap -> Save errors
	seg.OnSessionEnd(rec)                             // flush final lap -> Save errors
	if errs == 0 {
		t.Error("OnError not called on lap save failure")
	}
}
