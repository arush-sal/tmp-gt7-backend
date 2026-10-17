package live

import (
	"math"
	"sync"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// minRefSamples/minRefDistance gate reference promotion so a degenerate lap (sensor glitch, a lap
// sampled at a couple of near-identical positions) can't become the reference — that would set refBest
// but leave refTotal ~0 and permanently block Delta().
const (
	minRefSamples  = 10
	minRefDistance = 1.0 // metres
)

// DeltaTracker computes a live gap to the driver's best lap's pace at the same point on track. It is
// fed every frame on the ingest goroutine (single producer) and records the current lap's
// (cumulative-distance, elapsed) samples; on a lap boundary it promotes the just-completed lap to the
// REFERENCE iff it was the fastest so far (by GT7 last_lap). Reads via Delta() are concurrent.
type DeltaTracker struct {
	mu sync.Mutex

	// current lap accumulation
	lap      int32
	started  bool
	lapStart time.Time
	prevX    float32
	prevZ    float32
	havePrev bool
	distance float64
	cur      []processing.Sample // reuse Sample: Distance + RecvTime are what we need

	// reference (fastest completed lap so far)
	ref      processing.NormalizedLap
	refTotal float64
	refBest  time.Duration // GT7 last_lap of the reference lap (0 = none yet)

	latest    int64 // most recent delta ns
	haveDelta bool
}

// NewDeltaTracker returns an empty tracker with no reference lap yet.
func NewDeltaTracker() *DeltaTracker { return &DeltaTracker{} }

// Publish records one in-race frame and updates the latest delta. Non-blocking, single-producer
// (the ingest goroutine). Menu/pre-race frames, gap frames, and session resets are handled so bogus
// state never seeds the tracker. SINGLE-ACTIVE-SESSION assumption: one collector connection at a time
// (the MVP). Concurrent streams would interleave into this one global tracker — out of scope.
func (d *DeltaTracker) Publish(f *telemetryv1.TelemetryFrame) {
	if !session.OnTrack(f) { // ignore menu frames; they carry no meaningful lap distance
		return
	}
	ts := f.GetRecvTimestamp().AsTime()
	lap := f.GetCurrentLap()
	x, z := f.GetPositionX(), f.GetPositionZ()

	d.mu.Lock()
	defer d.mu.Unlock()

	switch {
	case !d.started || lap < d.lap: // first in-race frame, OR a session reset (lap counter went backwards)
		d.started = true
		d.lap = lap
		d.lapStart = ts
		d.distance = 0
		d.havePrev = false
		d.cur = nil
		d.haveDelta = false
		// A lap-counter decrease means a new session (new race, possibly a different car/track), so the
		// previous session's best lap is no longer a valid comparison point. Drop the reference too, or
		// Delta() would keep comparing the new session against a stale, unrelated best lap.
		d.ref = processing.NormalizedLap{}
		d.refTotal = 0
		d.refBest = 0
	case lap > d.lap: // lap boundary: promote the just-completed lap iff fastest AND non-degenerate, then reset
		if lastLap := f.GetLastLap().AsDuration(); lastLap > 0 && (d.refBest == 0 || lastLap < d.refBest) && len(d.cur) >= minRefSamples {
			if norm := processing.Normalize(d.cur); norm.TotalDistance >= minRefDistance {
				d.ref = norm
				d.refTotal = norm.TotalDistance
				d.refBest = lastLap
			}
		}
		d.lap = lap
		d.lapStart = ts
		d.distance = 0
		d.havePrev = false
		d.cur = nil
		d.haveDelta = false
	}

	// Accumulate distance, but skip the segment across a packet gap (its dx,dz would be a teleport-sized
	// spike): treat the post-gap frame as a fresh prev without integrating the jump.
	if d.havePrev && !f.GetGap() {
		dx, dz := float64(x-d.prevX), float64(z-d.prevZ)
		d.distance += math.Hypot(dx, dz)
	}
	d.prevX, d.prevZ, d.havePrev = x, z, true
	elapsed := ts.Sub(d.lapStart)
	d.cur = append(d.cur, processing.Sample{Distance: d.distance, RecvTime: ts})

	// delta vs the reference at the same track position (fraction of the reference's total distance)
	if d.refBest > 0 && d.refTotal > 0 {
		frac := d.distance / d.refTotal
		if refAt, ok := d.ref.TimeAt(frac); ok {
			refElapsed := refAt.Sub(d.ref.Points[0].RecvTime)
			d.latest = int64(elapsed - refElapsed)
			d.haveDelta = true
		}
	}
}

// Delta returns the latest live delta in ns (positive = slower than best pace here). ok is false until
// a reference lap exists and the current position maps onto it.
func (d *DeltaTracker) Delta() (ns int64, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.latest, d.haveDelta
}
