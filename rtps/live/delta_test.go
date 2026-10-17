package live

import (
	"sync"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// dframe builds a live frame at (x from distance along +X), recv time, lap, and optional last_lap.
// InRace defaults to true; use dframeNotInRace/dframeGap for the other guard tests.
func dframe(x float32, ts time.Time, lap int32, last time.Duration) *telemetryv1.TelemetryFrame {
	f := &telemetryv1.TelemetryFrame{
		PositionX:     x,
		CurrentLap:    lap,
		RecvTimestamp: timestamppb.New(ts),
		InRace:        true,
	}
	if last > 0 {
		f.LastLap = durationpb.New(last)
	}
	return f
}

// dframeGap is dframe with the packet-gap flag set (used to test the teleport-spike guard).
func dframeGap(x float32, ts time.Time, lap int32) *telemetryv1.TelemetryFrame {
	f := dframe(x, ts, lap, 0)
	f.Gap = true
	return f
}

// dframeNotInRace is a menu frame: InRace=false and GT7's menu lap sentinel (-1). The lap argument is
// ignored (kept for call-site symmetry): a frame with lap >= 0 counts as on track (session.OnTrack).
func dframeNotInRace(x float32, ts time.Time, _ int32) *telemetryv1.TelemetryFrame {
	f := dframe(x, ts, -1, 0)
	f.InRace = false
	return f
}

// TestDeltaTracker_LapGateWithoutInRace: newer GT7 builds leave in_race clear while racing; a frame
// with lap >= 0 must still be tracked.
func TestDeltaTracker_LapGateWithoutInRace(t *testing.T) {
	d := NewDeltaTracker()
	f := dframe(0, time.Unix(1_700_000_000, 0), 1, 0)
	f.InRace = false
	d.Publish(f)
	d.mu.Lock()
	started := d.started
	d.mu.Unlock()
	if !started {
		t.Fatal("on-track frame (lap >= 0, in_race clear) was ignored")
	}
}

func TestDeltaTracker_NoReference(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	d.Publish(dframe(0, base, 1, 0))
	d.Publish(dframe(10, base.Add(time.Second), 1, 0))
	if _, ok := d.Delta(); ok {
		t.Error("Delta ok=true before any reference lap")
	}
}

// buildReferenceLap feeds an 11-point (>=minRefSamples), 100m (>=minRefDistance) lap 1 at 10 m/s, then
// the lap-boundary frame (lap 1->2, last_lap=10s) that promotes it to the reference. Returns the
// boundary frame's timestamp (start of lap 2) for the caller to build lap-2 frames from.
func buildReferenceLap(d *DeltaTracker, base time.Time) time.Time {
	for i := 0; i <= 10; i++ {
		d.Publish(dframe(float32(i*10), base.Add(time.Duration(i)*time.Second), 1, 0))
	}
	start2 := base.Add(10 * time.Second)
	d.Publish(dframe(0, start2, 2, 10*time.Second))
	return start2
}

func TestDeltaTracker_AheadOfSlowerReference(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	start2 := buildReferenceLap(d, base)
	// Lap 2, faster (20 m/s): at 50m we're at t+2.5s vs reference's t+5s -> delta ~= -2.5s.
	d.Publish(dframe(50, start2.Add(2500*time.Millisecond), 2, 0))
	ns, ok := d.Delta()
	if !ok {
		t.Fatal("Delta ok=false after a reference lap + a mapped position")
	}
	if ns >= 0 {
		t.Errorf("delta = %v, want negative (ahead of the slower reference)", time.Duration(ns))
	}
}

// TestDeltaTracker_SlowerLapDoesNotReplaceFasterReference guards reference promotion: after a fast
// reference lap, a subsequent slower completed lap must not replace it. Removing the
// "lastLap < d.refBest" condition would let the slow lap 3 overwrite the fast lap-2 reference, and this
// test would then observe a much larger (near-zero) delta on lap 4 instead of the expected large
// positive delta measured against the fast reference.
func TestDeltaTracker_SlowerLapDoesNotReplaceFasterReference(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	// Lap 1: 100m over 10s (10 m/s) -> not fastest yet, but establishes the pattern.
	// We instead directly make lap 1 the fast reference at 20 m/s (100m/5s).
	for i := 0; i <= 10; i++ {
		d.Publish(dframe(float32(i*10), base.Add(time.Duration(i)*500*time.Millisecond), 1, 0))
	}
	start2 := base.Add(5 * time.Second)
	d.Publish(dframe(0, start2, 2, 5*time.Second)) // lap1 -> reference, refBest=5s

	// Lap 2: slow, 100m over 20s (5 m/s). Completing it must NOT replace the 5s reference.
	for i := 0; i <= 10; i++ {
		d.Publish(dframe(float32(i*10), start2.Add(time.Duration(i)*2*time.Second), 2, 0))
	}
	start3 := start2.Add(20 * time.Second)
	d.Publish(dframe(0, start3, 3, 20*time.Second)) // lap2 completes, slower -> must not replace ref

	// Lap 3, at reference pace (20 m/s): at 50m (t+2.5s) delta should be ~0, not deeply negative
	// (which would indicate comparison against the slow 20s lap instead of the 5s reference).
	d.Publish(dframe(50, start3.Add(2500*time.Millisecond), 3, 0))
	ns, ok := d.Delta()
	if !ok {
		t.Fatal("Delta ok=false after two completed laps + a mapped position")
	}
	want := 0.0
	got := time.Duration(ns).Seconds()
	if got < want-1 || got > want+1 {
		t.Errorf("delta = %v, want ~0 (measured against the fast lap-1 reference, not the slow lap-2)", time.Duration(ns))
	}
}

// TestDeltaTracker_DegenerateLapNotPromoted guards the minRefSamples/minRefDistance check: a completed
// lap with too few samples (or ~0 distance) must not become the reference. Without the guard, this
// degenerate "lap" would set refBest+refTotal~0, and frac = distance/refTotal would blow up (or the
// bogus reference would otherwise corrupt subsequent deltas) instead of leaving Delta() ok=false until
// a real lap completes.
func TestDeltaTracker_DegenerateLapNotPromoted(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	// Lap 1: only 2 samples, both at position 0 (0 distance) -- degenerate.
	d.Publish(dframe(0, base, 1, 0))
	d.Publish(dframe(0, base.Add(time.Second), 1, 0))
	// Lap boundary: lap 1 completes with a "last_lap" but must be rejected as reference.
	start2 := base.Add(2 * time.Second)
	d.Publish(dframe(0, start2, 2, 2*time.Second))

	if _, ok := d.Delta(); ok {
		t.Fatal("Delta ok=true after only a degenerate lap completed; degenerate lap must not become the reference")
	}

	// Now complete a real lap 2 (11 samples, 100m) to establish a real reference.
	for i := 0; i <= 10; i++ {
		d.Publish(dframe(float32(i*10), start2.Add(time.Duration(i)*time.Second), 2, 0))
	}
	start3 := start2.Add(10 * time.Second)
	d.Publish(dframe(0, start3, 3, 10*time.Second))
	d.Publish(dframe(50, start3.Add(5*time.Second), 3, 0))
	if _, ok := d.Delta(); !ok {
		t.Fatal("Delta ok=false after a real reference lap completed")
	}
}

// TestDeltaTracker_GapFrameNoSpike guards the Gap-skip on distance integration: a frame with Gap=true
// and a huge position jump must not have that jump integrated into distance. Without the `!f.GetGap()`
// guard, the teleport-sized dx would be folded straight into cumulative distance via math.Hypot.
// (The frame after the gap becomes the new "known good" baseline per the brief's design, so we assert
// on distance right after the gap frame rather than chaining a further frame off the gap's own bogus
// position -- that would conflate this guard with the separate, expected post-gap re-basing.)
func TestDeltaTracker_GapFrameNoSpike(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	start2 := buildReferenceLap(d, base) // 100m/10s reference

	// Lap 2: normal frame at 10m (distance becomes 10), then a gap frame that jumps to 10000m --
	// that jump must not be integrated.
	d.Publish(dframe(10, start2.Add(500*time.Millisecond), 2, 0))
	d.mu.Lock()
	beforeGap := d.distance
	d.mu.Unlock()

	d.Publish(dframeGap(10000, start2.Add(600*time.Millisecond), 2))
	d.mu.Lock()
	afterGap := d.distance
	d.mu.Unlock()

	if afterGap != beforeGap {
		t.Errorf("distance = %v after gap frame, want unchanged from %v (teleport must not be integrated)", afterGap, beforeGap)
	}
}

// TestDeltaTracker_SessionReset guards the "lap < d.lap" branch: current_lap going backwards (e.g.
// 8 -> 1) must reset the tracker so no cross-session distance/elapsed carries over. Without this guard,
// the old high lap counter would make every subsequent frame look like a lap boundary (lap > d.lap
// never true again after wraparound) or would otherwise mix pre-reset distance into the new session.
func TestDeltaTracker_SessionReset(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	// Drive lap 8 forward with real distance.
	for i := 0; i <= 5; i++ {
		d.Publish(dframe(float32(i*100), base.Add(time.Duration(i)*time.Second), 8, 0))
	}
	// Session reset: current_lap goes backwards to 1.
	resetTs := base.Add(10 * time.Second)
	d.Publish(dframe(0, resetTs, 1, 0))

	d.mu.Lock()
	gotLap, gotDist, gotHaveDelta := d.lap, d.distance, d.haveDelta
	d.mu.Unlock()
	if gotLap != 1 {
		t.Errorf("lap = %d, want 1 after session reset", gotLap)
	}
	if gotDist != 0 {
		t.Errorf("distance = %v, want 0 after session reset", gotDist)
	}
	if gotHaveDelta {
		t.Error("haveDelta = true immediately after session reset, want false")
	}
}

// TestDeltaTracker_NonInRaceIgnored guards the top-of-Publish InRace check: frames with InRace=false
// must not seed distance/lap state. Without the guard, a menu/replay-paused frame's position would get
// folded into the lap's distance accumulation.
func TestDeltaTracker_NonInRaceIgnored(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	// Menu frames at wildly different positions/laps before any real racing starts.
	d.Publish(dframeNotInRace(9999, base, 99))
	d.Publish(dframeNotInRace(0, base.Add(time.Second), 1))

	d.mu.Lock()
	started := d.started
	d.mu.Unlock()
	if started {
		t.Fatal("started = true after only non-in-race frames; they must be ignored entirely")
	}

	// Now race for real; state must reflect only the real frames, not the ignored ones.
	d.Publish(dframe(0, base.Add(2*time.Second), 1, 0))
	d.mu.Lock()
	gotLap, gotDist := d.lap, d.distance
	d.mu.Unlock()
	if gotLap != 1 || gotDist != 0 {
		t.Errorf("lap=%d distance=%v after first real in-race frame, want lap=1 distance=0", gotLap, gotDist)
	}
}

// TestDeltaTracker_FracAboveOneTail verifies the documented degraded-tail behaviour: past the
// reference's total distance, delta keeps comparing against the reference's final point rather than
// panicking or returning ok=false.
func TestDeltaTracker_FracAboveOneTail(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	start2 := buildReferenceLap(d, base) // 100m/10s reference

	// Lap 2 overruns the reference's total distance (150m > 100m reference total).
	d.Publish(dframe(150, start2.Add(6*time.Second), 2, 0))
	ns, ok := d.Delta()
	if !ok {
		t.Fatal("Delta ok=false past the reference's total distance; want degraded tail, not ok=false")
	}
	// Tail comparison: refElapsed == reference's last point elapsed (10s from ref start) since
	// TimeAt clamps frac>1 to the final point.
	wantRefElapsed := 10 * time.Second
	elapsed := 6 * time.Second
	wantNs := int64(elapsed - wantRefElapsed)
	if ns != wantNs {
		t.Errorf("delta = %v, want %v (tail comparison against reference's final point)", time.Duration(ns), time.Duration(wantNs))
	}
}

// TestDeltaTracker_SessionResetClearsReference guards the reset branch's reference clear: a session
// reset (lap counter going backwards) must drop d.ref/d.refTotal/d.refBest along with the in-progress
// lap state, since a new session may be a different car/track and the old best lap is no longer a valid
// comparison point. Without clearing the reference, Delta() would keep reporting ok=true against the
// stale pre-reset reference even though no lap in the new session has completed yet.
func TestDeltaTracker_SessionResetClearsReference(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	start2 := buildReferenceLap(d, base) // establishes a real reference (lap 1, 100m/10s)

	// Confirm the reference is live before the reset.
	d.Publish(dframe(50, start2.Add(2500*time.Millisecond), 2, 0))
	if _, ok := d.Delta(); !ok {
		t.Fatal("Delta ok=false before session reset; reference should be usable")
	}

	// Session reset: current_lap goes backwards (e.g. new race started).
	resetTs := start2.Add(5 * time.Second)
	d.Publish(dframe(0, resetTs, 1, 0))
	if _, ok := d.Delta(); ok {
		t.Fatal("Delta ok=true right after session reset; reference must be cleared")
	}

	// The stale reference must stay gone until a new reference lap completes -- feeding more frames on
	// the new session's lap 1 (no reference yet) must not resurrect a delta.
	d.Publish(dframe(20, resetTs.Add(time.Second), 1, 0))
	d.Publish(dframe(40, resetTs.Add(2*time.Second), 1, 0))
	if _, ok := d.Delta(); ok {
		t.Fatal("Delta ok=true while accumulating the new session's first lap; no reference should exist yet")
	}

	// Now build a real reference lap in the new session; Delta() should become usable again.
	newStart2 := buildReferenceLap(d, resetTs)
	d.Publish(dframe(50, newStart2.Add(2500*time.Millisecond), 2, 0))
	if _, ok := d.Delta(); !ok {
		t.Fatal("Delta ok=false after a fresh reference lap completed in the new session")
	}
}

// TestDeltaTracker_LowDistanceLapNotPromoted guards the minRefDistance gate in isolation from the
// minRefSamples gate: a completed lap with enough samples (>=minRefSamples) but total distance below
// minRefDistance (near-identical positions -- e.g. stationary in a menu/replay glitch) must not be
// promoted to the reference. Removing the `Normalize(cur).TotalDistance >= minRefDistance` check would
// let this lap set refBest with refTotal~0, corrupting frac = distance/refTotal on the next lap.
func TestDeltaTracker_LowDistanceLapNotPromoted(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	// Lap 1: 12 samples (>= minRefSamples), but positions barely move -- total distance well under
	// minRefDistance (1.0m). last_lap is positive so only the distance gate is exercised.
	for i := 0; i <= 11; i++ {
		d.Publish(dframe(float32(i)*0.05, base.Add(time.Duration(i)*time.Second), 1, 0))
	}
	start2 := base.Add(12 * time.Second)
	d.Publish(dframe(0, start2, 2, 12*time.Second))

	if _, ok := d.Delta(); ok {
		t.Fatal("Delta ok=true after only a low-distance lap completed; it must not become the reference")
	}

	// A real lap completing afterwards must be the one that becomes the reference.
	for i := 0; i <= 10; i++ {
		d.Publish(dframe(float32(i*10), start2.Add(time.Duration(i)*time.Second), 2, 0))
	}
	start3 := start2.Add(10 * time.Second)
	d.Publish(dframe(0, start3, 3, 10*time.Second))
	d.Publish(dframe(50, start3.Add(5*time.Second), 3, 0))
	if _, ok := d.Delta(); !ok {
		t.Fatal("Delta ok=false after a real reference lap completed")
	}
}

// TestDeltaTracker_ConcurrentRead exercises Publish (single producer) and Delta (concurrent reader)
// together under -race to confirm the mutex actually guards the shared state.
func TestDeltaTracker_ConcurrentRead(t *testing.T) {
	d := NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	buildReferenceLap(d, base)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				d.Delta()
			}
		}
	}()

	start2 := base.Add(10 * time.Second)
	for i := 1; i <= 50; i++ {
		d.Publish(dframe(float32(i), start2.Add(time.Duration(i)*10*time.Millisecond), 2, 0))
	}
	close(stop)
	wg.Wait()
}
