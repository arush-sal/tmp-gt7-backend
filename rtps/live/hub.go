// Package live is RTPS's real-time egress: it captures every decoded frame at the ingest seam and
// fans it out to live consumers (SSE) plus a latest-frame snapshot, independent of the processing/
// storage path. Egress here must never block the ingest goroutine (publish-first, D-B25).
package live

import (
	"sync"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
)

// frameState is the atomically-published latest frame plus the recv-time the current lap began at
// (for deriving running current-lap time). Immutable once stored.
type frameState struct {
	frame    *telemetryv1.TelemetryFrame
	lapStart time.Time
}

// Hub captures the latest frame and fans it out to subscribers. Publish is non-blocking; each
// subscriber holds a latest-wins slot (coalescing), so a slow consumer drops intermediate frames but
// never blocks the publisher — mirroring the collector's lossy live-publish channel.
type Hub struct {
	mu   sync.Mutex
	cur  *frameState
	subs map[*Sub]struct{}
}

// New returns an empty Hub.
func New() *Hub { return &Hub{subs: make(map[*Sub]struct{})} }

// Publish stores f as the latest and notifies every subscriber. Called on the ingest goroutine before
// Observe, so it must not block: subscriber delivery is a latest-slot swap + a non-blocking signal.
func (h *Hub) Publish(f *telemetryv1.TelemetryFrame) {
	h.mu.Lock()
	lapStart := f.GetRecvTimestamp().AsTime()
	if h.cur != nil && h.cur.frame.GetCurrentLap() == f.GetCurrentLap() {
		lapStart = h.cur.lapStart // same lap: keep the lap's original start
	}
	h.cur = &frameState{frame: f, lapStart: lapStart}
	subs := make([]*Sub, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	for _, s := range subs {
		s.put(f)
	}
}

// Latest returns the most recent frame and its lap-start; ok is false before the first Publish.
func (h *Hub) Latest() (*telemetryv1.TelemetryFrame, time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur == nil {
		return nil, time.Time{}, false
	}
	return h.cur.frame, h.cur.lapStart, true
}

// Subscribe registers a new subscriber. Call Close when done (e.g. on SSE client disconnect).
func (h *Hub) Subscribe() *Sub {
	s := &Sub{hub: h, notify: make(chan struct{}, 1)}
	h.mu.Lock()
	// Lazy-init defends a zero-value Hub{} (one not built via New()): its subs map is nil, and a
	// nil-map write panics. Publish's range and Latest already tolerate a nil map; only this write
	// needs the guard. No effect on the normal New() path (map is already non-nil).
	if h.subs == nil {
		h.subs = make(map[*Sub]struct{})
	}
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) unsubscribe(s *Sub) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

// Sub is one subscriber's latest-slot. put() overwrites the pending frame (coalesce) and signals;
// the consumer selects on C() then Take()s the freshest frame. The network write happens in the
// consumer's goroutine, never under a Hub lock.
type Sub struct {
	hub    *Hub
	mu     sync.Mutex
	latest *telemetryv1.TelemetryFrame
	notify chan struct{}
	closed bool
}

// put stores f as the pending frame and signals the consumer. It intentionally does not check
// s.closed — a Publish racing a Close() may write into a latest-slot no longer in the hub. That's
// harmless (no live reader, GC reclaims it, no panic); a lock here would only slow the hot path.
func (s *Sub) put(f *telemetryv1.TelemetryFrame) {
	s.mu.Lock()
	s.latest = f
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default: // already signalled and undrained; the latest-slot already holds the freshest
	}
}

// C is the readiness signal; a receive means "a fresher frame is waiting".
func (s *Sub) C() <-chan struct{} { return s.notify }

// Take returns and clears the pending frame (may be nil if already taken).
func (s *Sub) Take() *telemetryv1.TelemetryFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.latest
	s.latest = nil
	return f
}

// Close removes the subscriber from the hub.
func (s *Sub) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if s.hub != nil {
		s.hub.unsubscribe(s)
	}
}
