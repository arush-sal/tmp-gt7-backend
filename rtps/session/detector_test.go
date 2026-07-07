package session

import (
	"errors"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// base is an arbitrary fixed wall-clock origin; frame timestamps are offsets from it (in ms) so the
// debounce/min-session windows are exercised with readable numbers.
var base = time.Unix(1_700_000_000, 0)

// mkFrame builds a telemetry frame at base+ms with the fields the Detector actually reads. Physics
// (speed etc.) is intentionally omitted — the Detector is physics-blind.
func mkFrame(ms int, inRace bool, car int32, pkg uint32, curLap, totLaps int32) *telemetryv1.TelemetryFrame {
	return &telemetryv1.TelemetryFrame{
		RecvTimestamp: timestamppb.New(base.Add(time.Duration(ms) * time.Millisecond)),
		InRace:        inRace,
		CarId:         car,
		PackageId:     pkg,
		CurrentLap:    curLap,
		TotalLaps:     totLaps,
		Source:        telemetryv1.Source_SOURCE_LIVE,
	}
}

// capSink records the Detector's lifecycle calls so tests can assert on emitted sessions.
type capSink struct {
	starts []*Record
	ends   []*Record
	frames int
}

func (c *capSink) OnSessionStart(r *Record)                        { c.starts = append(c.starts, r) }
func (c *capSink) OnFrame(_ string, _ *telemetryv1.TelemetryFrame) { c.frames++ }
func (c *capSink) OnSessionEnd(r *Record)                          { c.ends = append(c.ends, r) }

// testCfg uses the real ~0.5s windows (D-B4) with a deterministic id generator so assertions are stable.
func testCfg() Config {
	n := 0
	return Config{
		Debounce:   500 * time.Millisecond,
		MinSession: 500 * time.Millisecond,
		NewID: func() string {
			n++
			return "sess-" + time.Duration(n).String()
		},
	}
}

// feed drives the Detector through the frames then simulates stream end (Close), returning the sink.
func feed(cfg Config, frames ...*telemetryv1.TelemetryFrame) (*capSink, *MemStore) {
	sink := &capSink{}
	store := NewMemStore()
	d := NewDetector(sink, store, cfg)
	for _, f := range frames {
		d.Observe(f)
	}
	d.Close()
	return sink, store
}

// inRaceRun returns count frames, all in-race, spaced stepMs apart starting at startMs, on one car,
// with monotonic package ids and the given lap counters.
func inRaceRun(startMs, stepMs, count int, car int32, pkg0 uint32, curLap, totLaps int32) []*telemetryv1.TelemetryFrame {
	out := make([]*telemetryv1.TelemetryFrame, count)
	for i := range count {
		out[i] = mkFrame(startMs+i*stepMs, true, car, pkg0+uint32(i), curLap, totLaps)
	}
	return out
}

// TestDetector_OpenCloseFinished: a stint longer than MinSession that ends in-menu closes as one
// finished session (current_lap >= total_laps).
func TestDetector_OpenCloseFinished(t *testing.T) {
	frames := inRaceRun(0, 100, 12, 1384, 100, 2, 2) // 0..1100ms in-race, lap 2/2 => finished
	frames = append(frames,
		mkFrame(1200, false, 1384, 112, -1, -1), // menu frame; starts close candidate
		mkFrame(1800, false, 1384, 113, -1, -1), // 600ms of false => close confirmed
	)
	sink, store := feed(testCfg(), frames...)

	if len(sink.starts) != 1 || len(sink.ends) != 1 {
		t.Fatalf("starts=%d ends=%d, want 1/1", len(sink.starts), len(sink.ends))
	}
	end := sink.ends[0]
	if end.Status != StatusFinished {
		t.Errorf("Status = %q, want finished", end.Status)
	}
	// EndTime must be the last in-race frame (1100ms), not the menu frames.
	if want := base.Add(1100 * time.Millisecond); !end.EndTime.Equal(want) {
		t.Errorf("EndTime = %v, want %v (last in-race frame)", end.EndTime, want)
	}
	if len(store.List()) != 1 {
		t.Errorf("store has %d sessions, want 1", len(store.List()))
	}
}

// TestDetector_ShortBlipDropped: the finish-line results flap — a ~0.1s in-race blip — must produce
// no session at all (never reaches MinSession).
func TestDetector_ShortBlipDropped(t *testing.T) {
	frames := []*telemetryv1.TelemetryFrame{
		mkFrame(0, true, 1384, 1, 1, 2),
		mkFrame(100, true, 1384, 2, 1, 2), // only 100ms in-race
		mkFrame(200, false, 1384, 3, -1, -1),
		mkFrame(900, false, 1384, 4, -1, -1), // 700ms false
	}
	sink, store := feed(testCfg(), frames...)
	if len(sink.starts) != 0 || len(sink.ends) != 0 {
		t.Errorf("blip emitted a session: starts=%d ends=%d, want 0/0", len(sink.starts), len(sink.ends))
	}
	if len(store.List()) != 0 {
		t.Errorf("store has %d sessions, want 0", len(store.List()))
	}
}

// TestDetector_Abandoned: closing with current_lap < total_laps tags the session abandoned (the quit
// case, e.g. capture race 3 at lap 1 of 10).
func TestDetector_Abandoned(t *testing.T) {
	frames := inRaceRun(0, 100, 12, 2108, 100, 1, 10) // lap 1/10
	frames = append(frames, mkFrame(1200, false, 2108, 200, -1, -1), mkFrame(1800, false, 2108, 201, -1, -1))
	sink, _ := feed(testCfg(), frames...)
	if len(sink.ends) != 1 {
		t.Fatalf("ends=%d, want 1", len(sink.ends))
	}
	if sink.ends[0].Status != StatusAbandoned {
		t.Errorf("Status = %q, want abandoned", sink.ends[0].Status)
	}
}

// TestDetector_PauseDoesNotSplit: is_paused stays in-race, so a long paused span must NOT close or
// split the session. Modeled as continuous in-race frames (pause never drops in_race).
func TestDetector_PauseDoesNotSplit(t *testing.T) {
	frames := inRaceRun(0, 100, 30, 2108, 100, 1, 10) // 0..2900ms continuous in-race incl. "pause"
	frames = append(frames, mkFrame(3000, false, 2108, 200, -1, -1), mkFrame(3600, false, 2108, 201, -1, -1))
	sink, _ := feed(testCfg(), frames...)
	if len(sink.starts) != 1 || len(sink.ends) != 1 {
		t.Errorf("pause split the session: starts=%d ends=%d, want 1/1", len(sink.starts), len(sink.ends))
	}
}

// TestDetector_BriefDropoutBridged: an in-race dropout shorter than Debounce must be bridged (not a
// close). in_race goes false for 200ms (< 500ms) then true again.
func TestDetector_BriefDropoutBridged(t *testing.T) {
	var frames []*telemetryv1.TelemetryFrame
	frames = append(frames, inRaceRun(0, 100, 8, 1384, 1, 2, 2)...) // 0..700ms in-race
	frames = append(frames,
		mkFrame(800, false, 1384, 9, 2, 2),  // dropout begins
		mkFrame(900, false, 1384, 10, 2, 2), // 200ms of false only
	)
	frames = append(frames, inRaceRun(1000, 100, 6, 1384, 11, 2, 2)...) // back in-race 1000..1500ms
	frames = append(frames, mkFrame(1600, false, 1384, 20, -1, -1), mkFrame(2200, false, 1384, 21, -1, -1))
	sink, _ := feed(testCfg(), frames...)
	if len(sink.starts) != 1 || len(sink.ends) != 1 {
		t.Errorf("brief dropout split the session: starts=%d ends=%d, want 1/1", len(sink.starts), len(sink.ends))
	}
}

// TestDetector_CarChangeSplits: a car_id change while active force-closes the current session and
// opens a new one (defensive boundary).
func TestDetector_CarChangeSplits(t *testing.T) {
	var frames []*telemetryv1.TelemetryFrame
	frames = append(frames, inRaceRun(0, 100, 10, 1384, 1, 2, 2)...)      // car A, 0..900ms
	frames = append(frames, inRaceRun(1000, 100, 10, 2108, 100, 2, 2)...) // car B, 1000..1900ms
	frames = append(frames, mkFrame(2000, false, 2108, 200, -1, -1), mkFrame(2600, false, 2108, 201, -1, -1))
	sink, store := feed(testCfg(), frames...)
	if len(store.List()) != 2 {
		t.Fatalf("store has %d sessions, want 2 (car change splits)", len(store.List()))
	}
	if sink.starts[0].CarID != 1384 || sink.starts[1].CarID != 2108 {
		t.Errorf("session cars = %d,%d, want 1384,2108", sink.starts[0].CarID, sink.starts[1].CarID)
	}
}

// TestDetector_CapturesCarName: the human-readable car_name resolved by the collector is snapshotted
// onto the Record when the session opens, alongside the numeric car id.
func TestDetector_CapturesCarName(t *testing.T) {
	const want = "Nissan Fairlady Z Version S (Z33)"
	frames := inRaceRun(0, 100, 12, 1384, 1, 2, 2)
	for _, f := range frames {
		f.CarName = want
	}
	sink, _ := feed(testCfg(), frames...)
	if len(sink.starts) != 1 {
		t.Fatalf("starts=%d, want 1", len(sink.starts))
	}
	if got := sink.starts[0].CarName; got != want {
		t.Errorf("Record.CarName = %q, want %q", got, want)
	}
}

// TestDetector_StreamEndFinalizes: if the stream ends (Close) while a session is still active, the
// session is finalized rather than lost.
func TestDetector_StreamEndFinalizes(t *testing.T) {
	frames := inRaceRun(0, 100, 12, 1384, 1, 2, 2) // ends in-race, no menu frames
	sink, store := feed(testCfg(), frames...)
	if len(sink.ends) != 1 {
		t.Fatalf("ends=%d, want 1 (Close finalizes)", len(sink.ends))
	}
	if len(store.List()) != 1 {
		t.Errorf("store has %d, want 1", len(store.List()))
	}
}

// TestDetector_MenuFrameNeverForcesBoundary pins a state-machine invariant: a differing car_id (or
// package_id) on an in_race=FALSE frame must NOT force-close the session — only the debounce path
// closes it. GT7 emits menu/results frames (which may report car_id=0) right after a race; treating
// those as a "car change" would close via the wrong path and bypass the debounce that bridges the
// results-screen flap. Only a real in-race car swap should force a boundary.
//
// Scenario: an in-race stint, then a single car_id=0 menu frame during the debounce window, then the
// stint resumes in-race on the original car. Correct behavior = one continuous session (the false
// frame is debounced, the resume cancels the pending close). The pre-fix code force-closed on the
// car=0 frame and opened a second session.
func TestDetector_MenuFrameNeverForcesBoundary(t *testing.T) {
	var frames []*telemetryv1.TelemetryFrame
	frames = append(frames, inRaceRun(0, 100, 8, 1384, 1, 2, 2)...)    // 0..700ms in-race, confirms at 500ms
	frames = append(frames, mkFrame(800, false, 0, 9, -1, -1))         // car=0 menu frame, 100ms into a close
	frames = append(frames, inRaceRun(900, 100, 8, 1384, 10, 2, 2)...) // resumes in-race, same car
	frames = append(frames, mkFrame(1700, false, 1384, 20, -1, -1), mkFrame(2300, false, 1384, 21, -1, -1))
	sink, store := feed(testCfg(), frames...)
	if len(store.List()) != 1 {
		t.Errorf("menu car=0 frame split the session: %d sessions, want 1", len(store.List()))
	}
	if len(sink.starts) != 1 {
		t.Errorf("starts=%d, want 1", len(sink.starts))
	}
}

// TestDetector_CloseIdempotent: calling Close after the session already closed is a harmless no-op
// (the ingest server may call Close on stream end even when a debounce already finalized the session).
func TestDetector_CloseIdempotent(t *testing.T) {
	sink := &capSink{}
	store := NewMemStore()
	d := NewDetector(sink, store, testCfg())
	for _, f := range inRaceRun(0, 100, 12, 1384, 1, 2, 2) {
		d.Observe(f)
	}
	d.Close()
	d.Close() // second call must not emit or panic
	if len(sink.ends) != 1 {
		t.Errorf("ends=%d after double Close, want 1", len(sink.ends))
	}
}

// errStore fails every Save — models a DB outage.
type errStore struct{ saves int }

func (e *errStore) Save(*Record) error         { e.saves++; return errors.New("boom") }
func (e *errStore) Get(string) (*Record, bool) { return nil, false }
func (e *errStore) List() []*Record            { return nil }

// TestDetector_SaveErrorNotConfirmed: if persisting the active session fails, the session must NOT be
// confirmed (no OnSessionStart — which would cascade into FK-violating lap inserts), and the failure
// must be surfaced via OnError.
func TestDetector_SaveErrorNotConfirmed(t *testing.T) {
	var errs int
	cfg := testCfg()
	cfg.OnError = func(error) { errs++ }
	sink := &capSink{}
	d := NewDetector(sink, &errStore{}, cfg)
	for _, f := range inRaceRun(0, 100, 12, 1384, 1, 2, 2) {
		d.Observe(f)
	}
	d.Close()
	if len(sink.starts) != 0 {
		t.Errorf("starts=%d, want 0 (save failed -> not confirmed)", len(sink.starts))
	}
	if errs == 0 {
		t.Error("OnError not called on save failure")
	}
}

// TestDetector_MenuOnlyNoSession: frames that are never in-race produce nothing.
func TestDetector_MenuOnlyNoSession(t *testing.T) {
	frames := []*telemetryv1.TelemetryFrame{
		mkFrame(0, false, 0, 1, 0, 0),
		mkFrame(500, false, 0, 2, 0, 0),
		mkFrame(1000, false, 1384, 3, 0, 0),
	}
	sink, store := feed(testCfg(), frames...)
	if len(sink.starts) != 0 || len(store.List()) != 0 {
		t.Errorf("menu-only produced sessions: starts=%d store=%d, want 0/0", len(sink.starts), len(store.List()))
	}
}
