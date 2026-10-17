package session

import (
	"errors"
	"strings"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// base is an arbitrary fixed wall-clock origin; frame timestamps are offsets from it (in ms) so the
// debounce/min-session windows are exercised with readable numbers.
var base = time.Unix(1_700_000_000, 0)

// mkFrame builds a telemetry frame at base+ms with the fields the Detector actually reads. In-race
// frames carry a driving speed so they satisfy the must-move confirmation rule; other physics is
// omitted.
func mkFrame(ms int, inRace bool, car int32, pkg uint32, curLap, totLaps int32) *telemetryv1.TelemetryFrame {
	var speed float32
	if inRace {
		speed = 100 // km/h: comfortably above minMovingSpeedKPH
	}
	return &telemetryv1.TelemetryFrame{
		CarSpeed:      speed,
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

// rolled gives a stint that starts on lap L >= 1 one completed timed lap, so it is stored rather than
// discarded as lapless: frames 0-5 stay on L (the session confirms at frame 5 = 500ms, mid-lap, so L is
// a partial lap), then the rest split into L+1 (a full lap) and L+2. Needs >= 8 frames.
func rolled(frames []*telemetryv1.TelemetryFrame) []*telemetryv1.TelemetryFrame {
	rest := frames[6:]
	for i, f := range rest {
		f.CurrentLap++
		if i >= len(rest)/2 {
			f.CurrentLap++
		}
	}
	return frames
}

// fromLap0 puts the first 6 frames (through the 500ms confirm) on lap 0, so the session is confirmed
// on the formation lap and its first lap >= 1 is a full timed lap.
func fromLap0(frames []*telemetryv1.TelemetryFrame) []*telemetryv1.TelemetryFrame {
	for _, f := range frames[:6] {
		f.CurrentLap = 0
	}
	return frames
}

// TestDetector_OpenCloseFinished: a stint longer than MinSession that ends in-menu closes as one
// finished session (current_lap >= total_laps).
func TestDetector_OpenCloseFinished(t *testing.T) {
	frames := rolled(inRaceRun(0, 100, 12, 1384, 100, 2, 2)) // 0..1100ms in-race, laps 2 -> 3 of 2 => finished
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
	frames = append(frames, rolled(inRaceRun(0, 100, 10, 1384, 1, 2, 2))...)      // car A, 0..900ms
	frames = append(frames, rolled(inRaceRun(1000, 100, 10, 2108, 100, 2, 2))...) // car B, 1000..1900ms
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
	frames := rolled(inRaceRun(0, 100, 12, 1384, 1, 2, 2)) // ends in-race, no menu frames
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
	frames = append(frames, inRaceRun(0, 100, 8, 1384, 1, 2, 2)...)            // 0..700ms in-race, confirms at 500ms
	frames = append(frames, mkFrame(800, false, 0, 9, -1, -1))                 // car=0 menu frame, 100ms into a close
	frames = append(frames, rolled(inRaceRun(900, 100, 8, 1384, 10, 3, 2))...) // resumes: 2 -> 3 partial, then 3 -> 4 completes a lap
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
func (e *errStore) Delete(string) error        { return nil }

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

// TestDetector_DefaultIDIsUUID: with a nil cfg.NewID, NewDetector must default to a globally-unique
// UUID generator — not the old monotonic "sess-N" counter. Two independently constructed Detectors
// must mint distinct ids (the counter reset to sess-1 per instance, so it collided across restarts).
// We inspect the defaulted generator through the same-package d.cfg seam the existing tests rely on.
func TestDetector_DefaultIDIsUUID(t *testing.T) {
	d1 := NewDetector(&capSink{}, NewMemStore(), Config{})
	d2 := NewDetector(&capSink{}, NewMemStore(), Config{})
	id1 := d1.cfg.NewID()
	id2 := d2.cfg.NewID()

	if strings.HasPrefix(id1, "sess-") {
		t.Errorf("default id %q still uses the old sess-N format", id1)
	}
	if id1 == id2 {
		t.Errorf("two Detectors minted the same id %q; ids must be globally unique", id1)
	}
	if _, err := uuid.Parse(id1); err != nil {
		t.Errorf("default id %q is not a valid UUID: %v", id1, err)
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

// TestOnTrack: a frame is "driving" when GT7's in_race flag is set OR the lap counter is >= 0. Newer
// GT7 builds leave in_race clear even while racing; menus report current_lap = -1.
func TestOnTrack(t *testing.T) {
	cases := []struct {
		inRace bool
		lap    int32
		want   bool
	}{
		{true, -1, true},
		{false, 0, true},
		{false, 3, true},
		{false, -1, false},
	}
	for _, c := range cases {
		f := &telemetryv1.TelemetryFrame{InRace: c.inRace, CurrentLap: c.lap}
		if got := OnTrack(f); got != c.want {
			t.Errorf("OnTrack(in_race=%v, lap=%d) = %v, want %v", c.inRace, c.lap, got, c.want)
		}
	}
}

// moving returns frames with in_race clear (newer GT7 builds) but a lap counter >= 0 and a driving
// speed, spaced stepMs apart.
func moving(startMs, stepMs, count int, car int32, pkg0 uint32, lap, tot int32) []*telemetryv1.TelemetryFrame {
	out := inRaceRun(startMs, stepMs, count, car, pkg0, lap, tot)
	for _, f := range out {
		f.InRace = false
	}
	return out
}

// TestDetector_LapGateWithoutInRace: in_race=false but lap >= 0 and moving is a driving session.
func TestDetector_LapGateWithoutInRace(t *testing.T) {
	frames := rolled(moving(0, 100, 12, 396, 1, 1, 10))
	frames = append(frames, mkFrame(1200, false, 396, 50, -1, -1), mkFrame(1800, false, 396, 51, -1, -1))
	sink, store := feed(testCfg(), frames...)
	if len(sink.starts) != 1 || len(sink.ends) != 1 || len(store.List()) != 1 {
		t.Fatalf("starts=%d ends=%d stored=%d, want 1/1/1", len(sink.starts), len(sink.ends), len(store.List()))
	}
}

// TestDetector_StationaryNeverConfirms: on-track frames (lap >= 0) with the car stationary for well
// beyond MinSession must not create a session (idle on the grid / in a pit box).
func TestDetector_StationaryNeverConfirms(t *testing.T) {
	frames := moving(0, 100, 50, 396, 1, 2, 0)
	for _, f := range frames {
		f.CarSpeed = 0
	}
	sink, store := feed(testCfg(), frames...)
	if len(sink.starts) != 0 || len(store.List()) != 0 {
		t.Fatalf("stationary idle produced sessions: starts=%d stored=%d", len(sink.starts), len(store.List()))
	}
}

// TestDetector_StationaryThenMovingConfirms: a session confirms once the car moves, even if it sat
// stationary first.
func TestDetector_StationaryThenMovingConfirms(t *testing.T) {
	frames := moving(0, 100, 10, 396, 1, 0, 10)
	for _, f := range frames {
		f.CarSpeed = 0
	}
	frames = append(frames, moving(1000, 100, 10, 396, 11, 0, 10)...)
	sink, _ := feed(testCfg(), frames...)
	if len(sink.starts) != 1 {
		t.Fatalf("starts=%d, want 1", len(sink.starts))
	}
}

// TestDetector_LapDecreaseSplits: the lap counter going backwards while on track (a replay loop or
// rewind) closes the session and opens a new one, even without a debounce-length menu gap.
func TestDetector_LapDecreaseSplits(t *testing.T) {
	frames := rolled(moving(0, 100, 20, 396, 1, 3, 0))                       // loop 1: laps 3, 4, 5
	frames = append(frames, mkFrame(2000, false, 396, 21, -1, -1))           // 1 menu frame < debounce
	frames = append(frames, rolled(moving(2100, 100, 10, 396, 22, 2, 0))...) // loop 2: laps 2, 3, 4
	sink, store := feed(testCfg(), frames...)
	if len(store.List()) != 2 || len(sink.ends) != 2 {
		t.Fatalf("stored=%d ends=%d, want 2/2 (lap 5 -> -1 -> 2 splits)", len(store.List()), len(sink.ends))
	}
}

// TestDetector_LapZeroNeverSplits: after a finish GT7 shows the results screen as on-track lap-0
// frames (lap N -> -1 -> 0, sometimes flapping back to N); a decrease to or from lap 0 is not a
// boundary, and the race keeps its final lap counters (lap 0 / total 0 frames don't overwrite them).
func TestDetector_LapZeroNeverSplits(t *testing.T) {
	frames := rolled(moving(0, 100, 10, 396, 1, 2, 2))               // laps 2, 3, 4 of 2: finished
	frames = append(frames, mkFrame(1000, false, 396, 11, -1, -1))   // 1 menu frame
	frames = append(frames, moving(1100, 100, 10, 396, 12, 0, 2)...) // results: lap 0
	frames = append(frames, moving(2100, 100, 3, 396, 22, 4, 2)...)  // flap back to lap 4
	frames = append(frames, moving(2400, 100, 10, 396, 25, 0, 0)...) // lap 0, total 0
	_, store := feed(testCfg(), frames...)
	got := store.List()
	if len(got) != 1 {
		t.Fatalf("stored=%d, want 1 (lap 0 frames never split)", len(got))
	}
	if r := got[0]; r.CurrentLap != 4 || r.TotalLaps != 2 || r.Status != StatusFinished {
		t.Errorf("record lap %d/%d status %s, want 4/2 finished", r.CurrentLap, r.TotalLaps, r.Status)
	}
}

// withClock sets time_on_track (GT7's in-game clock) on frames, advancing by stepMs from startMs.
func withClock(frames []*telemetryv1.TelemetryFrame, startMs, stepMs int) []*telemetryv1.TelemetryFrame {
	for i, f := range frames {
		f.TimeOnTrack = durationpb.New(time.Duration(startMs+i*stepMs) * time.Millisecond)
	}
	return frames
}

// TestDetector_ClockBackwardSplits: the in-game clock jumping backwards by more than the threshold
// between timed-lap frames (a replay rewind) splits the session. Jumps involving lap 0 never split:
// the ~19s lap 0 -> 1 rolling-start correction and the ~99.8s jump DURING the WTC rolling-start lap 0.
func TestDetector_ClockBackwardSplits(t *testing.T) {
	frames := withClock(rolled(moving(0, 100, 10, 396, 1, 1, 0)), 600_000, 100) // laps 1, 2, 3
	// Same lap 3, clock 31s back: only the clock rule can split here.
	frames = append(frames, withClock(rolled(moving(1000, 100, 10, 396, 11, 3, 0)), 600_000-31_000, 100)...)
	_, store := feed(testCfg(), frames...)
	if len(store.List()) != 2 {
		t.Fatalf("stored=%d, want 2 (31s clock rewind on lap 3 splits)", len(store.List()))
	}

	frames = withClock(moving(0, 100, 10, 396, 1, 0, 0), 600_000, 100)
	frames = append(frames, withClock(moving(1000, 100, 10, 396, 11, 0, 0), 600_000-99_800, 100)...) // in lap 0
	frames = append(frames, withClock(rolled(moving(2000, 100, 10, 396, 21, 1, 0)), 600_000-99_800-19_000, 100)...)
	_, store = feed(testCfg(), frames...)
	if len(store.List()) != 1 {
		t.Fatalf("stored=%d, want 1 (rolling-start clock jumps on lap 0 must not split)", len(store.List()))
	}
}

// TestDetector_NoCompletedLapDiscarded: a confirmed session that ends without a completed timed lap is
// rolled back through the discard path: its row (saved at confirm) is deleted, the sinks still get
// exactly one OnSessionEnd, and OnDiscard reports DiscardNoCompletedLap.
func TestDetector_NoCompletedLapDiscarded(t *testing.T) {
	h := &dupHooks{owner: map[string]string{}}
	sink := &capSink{}
	store := NewMemStore()
	d := NewDetector(sink, store, h.cfg())
	for _, f := range moving(0, 100, 20, 396, 1, 1, 10) { // lap 1 only (a quit during lap 1)
		d.Observe(f)
	}
	if len(store.List()) != 1 {
		t.Fatalf("pending stored=%d, want 1 (row saved at confirm)", len(store.List()))
	}
	d.Close()
	if len(store.List()) != 0 || len(sink.starts) != 1 || len(sink.ends) != 1 {
		t.Errorf("stored=%d starts=%d ends=%d, want 0/1/1", len(store.List()), len(sink.starts), len(sink.ends))
	}
	if h.noLap != 1 || len(h.dropped) != 0 {
		t.Errorf("no-lap discards=%d duplicate discards=%v, want 1/none", h.noLap, h.dropped)
	}
}

// fpFrame is a rollover frame: the first frame of lap `lap` with the completed lap's time.
func fpFrame(ms int, pkg uint32, lap int32, lastMs int64, x float32) *telemetryv1.TelemetryFrame {
	f := moving(ms, 1, 1, 396, pkg, lap, 10)[0]
	f.LastLap = durationpb.New(time.Duration(lastMs) * time.Millisecond)
	f.TimeOnTrack = durationpb.New(time.Duration(ms) * time.Millisecond)
	f.PositionX, f.PositionY, f.PositionZ = x, 1, 2
	return f
}

// TestLapFingerprint: identical frames hash identically; any identifying field changes the hash.
func TestLapFingerprint(t *testing.T) {
	a := LapFingerprint(fpFrame(1000, 1, 2, 92_192, 5), 1)
	if a == "" || a != LapFingerprint(fpFrame(1000, 99, 2, 92_192, 5), 1) {
		t.Fatalf("fingerprint not stable across package ids: %q", a)
	}
	for name, b := range map[string]string{
		"lap":   LapFingerprint(fpFrame(1000, 1, 2, 92_192, 5), 2),
		"time":  LapFingerprint(fpFrame(1000, 1, 2, 92_193, 5), 1),
		"clock": LapFingerprint(fpFrame(1001, 1, 2, 92_192, 5), 1),
		"pos":   LapFingerprint(fpFrame(1000, 1, 2, 92_192, 5.0001), 1),
	} {
		if b == a {
			t.Errorf("changing %s did not change the fingerprint", name)
		}
	}
}

// dupHooks records the Detector's duplicate callbacks and serves a fixed fingerprint index.
type dupHooks struct {
	owner    map[string]string // fingerprint -> owning session
	dropped  []string          // originalIDs of DiscardDuplicate discards
	noLap    int               // DiscardNoCompletedLap discards
	diverged []string
}

func (h *dupHooks) cfg() Config {
	c := testCfg()
	c.FingerprintOwner = func(fp string) (string, bool) { id, ok := h.owner[fp]; return id, ok }
	c.OnDiscard = func(_ *Record, reason DiscardReason, orig string) {
		if reason == DiscardDuplicate {
			h.dropped = append(h.dropped, orig)
		} else {
			h.noLap++
		}
	}
	c.OnDuplicateDiverged = func(_ *Record, orig string) { h.diverged = append(h.diverged, orig) }
	return c
}

// TestDetector_DuplicateDropped: a session whose completed lap's fingerprint already belongs to a
// different stored session is rolled back (session row deleted, sinks told the session ended) and
// none of its later frames reach the sinks or the store.
func TestDetector_DuplicateDropped(t *testing.T) {
	roll := fpFrame(1000, 11, 2, 92_192, 5)
	h := &dupHooks{owner: map[string]string{LapFingerprint(roll, 1): "orig"}}
	sink := &capSink{}
	store := NewMemStore()
	d := NewDetector(sink, store, h.cfg())
	for _, f := range fromLap0(moving(0, 100, 10, 396, 1, 1, 10)) { // confirms on lap 0, then lap 1
		d.Observe(f)
	}
	if len(store.List()) != 1 {
		t.Fatalf("pre-rollover stored=%d, want 1", len(store.List()))
	}
	framesBefore := sink.frames
	d.Observe(roll) // 1 -> 2: fingerprint matches "orig"
	for _, f := range moving(1100, 100, 10, 396, 12, 2, 10) {
		d.Observe(f)
	}
	d.Close()
	if len(store.List()) != 0 {
		t.Errorf("duplicate left %d stored sessions, want 0", len(store.List()))
	}
	if sink.frames != framesBefore {
		t.Errorf("sinks received %d frames after the drop, want 0", sink.frames-framesBefore)
	}
	if len(sink.ends) != 1 {
		t.Errorf("ends=%d, want 1 (sinks release the dropped session's state once)", len(sink.ends))
	}
	if len(h.dropped) != 1 || h.dropped[0] != "orig" {
		t.Errorf("duplicate discards = %v, want [orig]", h.dropped)
	}
	if len(h.diverged) != 0 {
		t.Errorf("diverged = %v, want none", h.diverged)
	}
}

// TestDetector_DuplicateDiverged: once dropped, a later lap whose fingerprint is NOT in the original
// keeps being dropped (no resurrection) and reports the divergence exactly once.
func TestDetector_DuplicateDiverged(t *testing.T) {
	roll := fpFrame(1000, 11, 2, 92_192, 5)
	h := &dupHooks{owner: map[string]string{LapFingerprint(roll, 1): "orig"}}
	sink := &capSink{}
	store := NewMemStore()
	d := NewDetector(sink, store, h.cfg())
	for _, f := range fromLap0(moving(0, 100, 10, 396, 1, 1, 10)) {
		d.Observe(f)
	}
	d.Observe(roll)
	d.Observe(fpFrame(2000, 12, 3, 95_000, 7)) // unknown fingerprint
	d.Observe(fpFrame(3000, 13, 4, 95_100, 8)) // unknown again: still one report
	d.Close()
	if len(h.diverged) != 1 || h.diverged[0] != "orig" {
		t.Errorf("diverged = %v, want [orig]", h.diverged)
	}
	if len(store.List()) != 0 || len(sink.starts) != 1 {
		t.Errorf("stored=%d starts=%d, want 0/1 (no resurrection)", len(store.List()), len(sink.starts))
	}
}

// TestDetector_UnknownFingerprintKept: a completed lap with no matching fingerprint is a normal session.
func TestDetector_UnknownFingerprintKept(t *testing.T) {
	h := &dupHooks{owner: map[string]string{}}
	frames := fromLap0(moving(0, 100, 10, 396, 1, 1, 10))
	frames = append(frames, fpFrame(1000, 11, 2, 92_192, 5))
	_, store := feed(h.cfg(), frames...)
	if len(store.List()) != 1 || len(h.dropped) != 0 {
		t.Errorf("stored=%d dropped=%v, want 1/none", len(store.List()), h.dropped)
	}
}

// setAsync issues a recording request from another goroutine (as the HTTP handler does) and returns
// its result channel; the test then drives frames so the Detector applies it.
type recResult struct {
	recording bool
	id        string
	ok        bool
}

func setAsync(c *RecordingControl, on bool) <-chan recResult {
	ch := make(chan recResult, 1)
	go func() {
		r, id, err := c.Set(on, "", 5*time.Second)
		ch <- recResult{r, id, err == nil}
	}()
	return ch
}

// waitPending blocks until c has a queued request (so the next Observe applies it).
func waitPending(t *testing.T, c *RecordingControl) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !c.pending.Load() {
		if time.Now().After(deadline) {
			t.Fatal("request never queued")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestRecording_IdleRejected: with no open session, Set reports !ok (the API's 409).
func TestRecording_IdleRejected(t *testing.T) {
	c := NewRecordingControl()
	if _, _, err := c.Set(false, "", time.Second); err != ErrNoSession {
		t.Fatalf("Set while idle = %v, want ErrNoSession", err)
	}
	if !c.Recording() {
		t.Error("Recording() = false while idle, want true (default)")
	}
}

// TestRecording_OffDiscardsOnStartsFresh: turning recording off mid-session discards the whole session
// (row deleted, OnDiscard with DiscardRecordingOff, no frames forwarded after); turning it back on
// while still on track opens a fresh session with a new id; at the next session the setting is on
// again.
func TestRecording_OffDiscardsOnStartsFresh(t *testing.T) {
	c := NewRecordingControl()
	cfg := testCfg()
	cfg.Recording = c
	var reasons []DiscardReason
	cfg.OnDiscard = func(_ *Record, r DiscardReason, _ string) { reasons = append(reasons, r) }
	sink := &capSink{}
	store := NewMemStore()
	d := NewDetector(sink, store, cfg)

	run := rolled(moving(0, 100, 20, 396, 1, 1, 10)) // laps 1, 2, 3: confirmed, one completed lap
	for _, f := range run {
		d.Observe(f)
	}
	first := sink.starts[0].ID
	if _, id := c.State(); id != first {
		t.Fatalf("control session id = %q, want %q", id, first)
	}

	// Idempotent: setting the current value is a no-op that returns at once.
	if r, id, err := c.Set(true, "", time.Second); err != nil || !r || id != first {
		t.Fatalf("Set(true) no-op = %v,%q,%v want true,%q,nil", r, id, err, first)
	}

	off := setAsync(c, false)
	waitPending(t, c)
	framesBefore := sink.frames
	for _, f := range moving(2000, 100, 5, 396, 21, 3, 10) {
		d.Observe(f)
	}
	if res := <-off; !res.ok || res.recording || res.id != "" {
		t.Fatalf("Set(false) = %+v, want recording=false id=\"\" ok", res)
	}
	if c.Recording() {
		t.Error("Recording() = true after off")
	}
	if len(store.List()) != 0 || len(reasons) != 1 || reasons[0] != DiscardRecordingOff {
		t.Fatalf("stored=%d reasons=%v, want 0/[recording off]", len(store.List()), reasons)
	}
	if sink.frames != framesBefore || len(sink.ends) != 1 {
		t.Errorf("after off: %d frames forwarded, ends=%d; want 0, 1", sink.frames-framesBefore, len(sink.ends))
	}

	on := setAsync(c, true)
	waitPending(t, c)
	for _, f := range rolled(moving(2500, 100, 20, 396, 26, 3, 10)) { // still on track: laps 3, 4, 5
		d.Observe(f)
	}
	res := <-on
	if !res.ok || !res.recording || res.id == "" || res.id == first {
		t.Fatalf("Set(true) = %+v, want recording=true and a NEW session id", res)
	}
	d.Close()
	got := store.List()
	if len(got) != 1 || got[0].ID != res.id {
		t.Fatalf("stored = %v, want only the fresh session %s", got, res.id)
	}

	// Not sticky: off, then the stint ends; the next session records again.
	d = NewDetector(sink, store, cfg)
	for _, f := range moving(10_000, 100, 10, 396, 100, 1, 10) {
		d.Observe(f)
	}
	off = setAsync(c, false)
	waitPending(t, c)
	d.Observe(moving(11_000, 100, 1, 396, 110, 1, 10)[0])
	<-off
	d.Observe(mkFrame(11_100, false, 396, 111, -1, -1))
	d.Observe(mkFrame(11_700, false, 396, 112, -1, -1)) // debounce closes the stint
	if !c.Recording() {
		t.Error("Recording() still false after the session ended; must reset to true")
	}
}

// recSetup opens a confirmed, recording session A on a fresh Detector with a RecordingControl.
func recSetup(t *testing.T) (*Detector, *RecordingControl, *capSink, *MemStore) {
	t.Helper()
	c := NewRecordingControl()
	cfg := testCfg()
	cfg.Recording = c
	sink, store := &capSink{}, NewMemStore()
	d := NewDetector(sink, store, cfg)
	for _, f := range rolled(moving(0, 100, 20, 396, 1, 1, 10)) {
		d.Observe(f)
	}
	if len(sink.starts) != 1 {
		t.Fatal("setup: session not confirmed")
	}
	return d, c, sink, store
}

// TestRecording_StalledWithdrawn (H2): when no frame arrives within the wait, Set returns ErrStalled
// and the request is withdrawn: later frames never apply it.
func TestRecording_StalledWithdrawn(t *testing.T) {
	d, c, _, store := recSetup(t)
	if _, _, err := c.Set(false, "", 20*time.Millisecond); err != ErrStalled {
		t.Fatalf("Set on a stalled stream = %v, want ErrStalled", err)
	}
	for _, f := range moving(2000, 100, 5, 396, 21, 3, 10) {
		d.Observe(f)
	}
	if !c.Recording() || len(store.List()) != 1 {
		t.Errorf("withdrawn request applied later: recording=%v stored=%d", c.Recording(), len(store.List()))
	}
}

// TestRecording_PendingDroppedAtSessionEnd (H2): a request still queued when the session ends (stream
// end here) is answered ErrSessionChanged and never applies to a later session.
func TestRecording_PendingDroppedAtSessionEnd(t *testing.T) {
	d, c, _, store := recSetup(t)
	off := setAsync(c, false)
	waitPending(t, c)
	d.Close()
	if res := <-off; res.ok {
		t.Fatalf("Set(false) pending at Close = %+v, want an error", res)
	}
	d2 := NewDetector(&capSink{}, store, d.cfg)
	for _, f := range rolled(moving(5000, 100, 20, 396, 100, 1, 10)) {
		d2.Observe(f)
	}
	d2.Close()
	if n := len(store.List()); n != 2 {
		t.Errorf("stored=%d, want 2 (both sessions kept; the dropped request never applied)", n)
	}
}

// TestRecording_TargetsShownSession (M4): an off request naming another session is ErrSessionChanged
// and changes nothing; naming the open session discards it.
func TestRecording_TargetsShownSession(t *testing.T) {
	d, c, sink, store := recSetup(t)
	a := sink.starts[0].ID
	if _, _, err := c.Set(false, "someone-else", time.Second); err != ErrSessionChanged {
		t.Fatalf("Set(off, other) = %v, want ErrSessionChanged", err)
	}
	if !c.Recording() || len(store.List()) != 1 {
		t.Fatal("mismatched off request changed state")
	}
	ch := make(chan error, 1)
	go func() { _, _, err := c.Set(false, a, 5*time.Second); ch <- err }()
	waitPending(t, c)
	d.Observe(moving(2000, 100, 1, 396, 21, 3, 10)[0])
	if err := <-ch; err != nil || len(store.List()) != 0 {
		t.Errorf("Set(off, %s) = %v, stored=%d; want nil, 0", a, err, len(store.List()))
	}
}

// TestRecording_DuplicateOfAccepted (M4): while the open session D is a dropped duplicate of stored X,
// the UI shows X; an off request naming X applies to D (never touches X).
func TestRecording_DuplicateOfAccepted(t *testing.T) {
	roll := fpFrame(1000, 11, 2, 92_192, 5)
	h := &dupHooks{owner: map[string]string{LapFingerprint(roll, 1): "X"}}
	cfg := h.cfg()
	c := NewRecordingControl()
	cfg.Recording = c
	d := NewDetector(&capSink{}, NewMemStore(), cfg)
	for _, f := range fromLap0(moving(0, 100, 10, 396, 1, 1, 10)) {
		d.Observe(f)
	}
	d.Observe(roll) // D becomes a duplicate of X
	ch := make(chan error, 1)
	go func() { _, _, err := c.Set(false, "X", 5*time.Second); ch <- err }()
	waitPending(t, c)
	d.Observe(moving(1100, 100, 1, 396, 12, 2, 10)[0])
	if err := <-ch; err != nil || c.Recording() {
		t.Errorf("Set(off, X) during duplicate = %v (recording=%v), want applied", err, c.Recording())
	}
}

// TestRecording_BoundaryRace (M4): the caller's check passed against session A, but A ended and B
// opened before the Detector took the request; the Detector's re-check turns it into a no-op ack
// (ErrSessionChanged) and B is kept.
func TestRecording_BoundaryRace(t *testing.T) {
	d, c, sink, store := recSetup(t)
	a := sink.starts[0].ID
	// A ends and B opens (car change), then the stale check: republish A as current, as the HTTP
	// goroutine may have observed it just before the boundary.
	for _, f := range rolled(moving(2000, 100, 20, 2108, 21, 1, 10)) {
		d.Observe(f)
	}
	c.publish(true, true, a, a, "")
	ch := make(chan error, 1)
	go func() { _, _, err := c.Set(false, a, 5*time.Second); ch <- err }()
	waitPending(t, c)
	d.Observe(moving(4000, 100, 1, 2108, 41, 3, 10)[0])
	if err := <-ch; err != ErrSessionChanged {
		t.Fatalf("stale off request = %v, want ErrSessionChanged", err)
	}
	d.Close()
	if n := len(store.List()); n != 2 {
		t.Errorf("stored=%d, want 2 (B must not be discarded)", n)
	}
}

// TestDetector_PartialFirstLapNotCompleted (M3): a session confirmed mid-lap (lap >= 1) does not count
// its first rollover as a completed timed lap: no dedup lookup, and if no further lap completes the
// session is dropped by rule B.
func TestDetector_PartialFirstLapNotCompleted(t *testing.T) {
	lookups := 0
	h := &dupHooks{owner: map[string]string{}}
	cfg := h.cfg()
	cfg.FingerprintOwner = func(string) (string, bool) { lookups++; return "", false }
	frames := moving(0, 100, 10, 396, 1, 2, 10) // joined mid lap 2
	frames = append(frames, moving(1000, 100, 5, 396, 11, 3, 10)...)
	_, store := feed(cfg, frames...)
	if lookups != 0 || len(store.List()) != 0 || h.noLap != 1 {
		t.Errorf("lookups=%d stored=%d noLap=%d, want 0/0/1", lookups, len(store.List()), h.noLap)
	}
}

// TestDetector_LateDuplicateKept (#5): only the first completed lap decides duplicates; a later lap
// matching another session is reported once and the session is kept.
func TestDetector_LateDuplicateKept(t *testing.T) {
	late := fpFrame(2000, 12, 3, 95_000, 7)
	h := &dupHooks{owner: map[string]string{LapFingerprint(late, 2): "other"}}
	cfg := h.cfg()
	var lateDup []string
	cfg.OnLateDuplicate = func(_ *Record, other string) { lateDup = append(lateDup, other) }
	frames := fromLap0(moving(0, 100, 10, 396, 1, 1, 10))
	frames = append(frames, fpFrame(1000, 11, 2, 92_192, 5), late, fpFrame(3000, 13, 4, 95_100, 8))
	_, store := feed(cfg, frames...)
	if len(store.List()) != 1 || len(h.dropped) != 0 {
		t.Errorf("stored=%d dropped=%v, want 1/none", len(store.List()), h.dropped)
	}
	if len(lateDup) != 1 || lateDup[0] != "other" {
		t.Errorf("late duplicates = %v, want [other]", lateDup)
	}
}

// TestDetector_ClockMidnightWrapNoSplit: a backward clock jump of more than 23h is GT7's time of day
// passing midnight, not a rewind (unverified against captures; see clockWrapThreshold).
func TestDetector_ClockMidnightWrapNoSplit(t *testing.T) {
	frames := withClock(rolled(moving(0, 100, 10, 396, 1, 1, 0)), 86_399_500, 100)
	for _, f := range frames[5:] { // past midnight from frame 5
		f.TimeOnTrack = durationpb.New(f.GetTimeOnTrack().AsDuration() - 24*time.Hour)
	}
	_, store := feed(testCfg(), frames...)
	if n := len(store.List()); n != 1 {
		t.Errorf("stored=%d, want 1 (midnight wrap must not split)", n)
	}
}

// TestRecording_PendingDroppedAtFinalize (H2): an untargeted off request queued after the Detector's
// per-frame take but before a boundary (finalize, then open of the next session in the same frame)
// must be dropped at finalize, never applied to the next session. Simulated by calling finalize while
// the request is queued, then letting the next session open.
func TestRecording_PendingDroppedAtFinalize(t *testing.T) {
	d, c, _, store := recSetup(t)
	off := setAsync(c, false)
	waitPending(t, c)
	next := rolled(moving(5000, 100, 20, 396, 100, 1, 10))
	d.finalize() // a split boundary arrives before the next take: finalize, then open in the same frame
	d.open(next[0], next[0].GetRecvTimestamp().AsTime())
	for _, f := range next[1:] {
		d.Observe(f)
	}
	if res := <-off; res.ok {
		t.Fatalf("Set(false) pending at finalize = %+v, want an error", res)
	}
	d.Close()
	if n := len(store.List()); n != 2 || !c.Recording() {
		t.Errorf("stored=%d recording=%v, want 2/true (request never applied to the next session)", n, c.Recording())
	}
}
