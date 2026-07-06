package live

import (
	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
)

// framePublisher is the minimal per-frame sink both *Hub and *DeltaTracker satisfy structurally. It is
// declared locally (rather than importing rtps/ingest.Publisher) to keep this package import-clean and
// avoid a dependency cycle (ingest already depends on live for the Hub).
type framePublisher interface {
	Publish(*telemetryv1.TelemetryFrame)
}

// MultiPublisher fans one ingest frame out to several framePublishers (e.g. the live Hub and the
// DeltaTracker), so ingest.Server.Publisher can remain a single field while wiring multiple
// egress-side consumers.
type MultiPublisher struct {
	pubs []framePublisher
}

// NewMultiPublisher returns a MultiPublisher that forwards each frame to every non-nil pub in pubs, in
// order. Fan-out order does not matter functionally: every publisher here is required to be
// non-blocking and independent of the others (no shared mutable state between them), so the order in
// which they observe a given frame is not observable to callers.
func NewMultiPublisher(pubs ...framePublisher) *MultiPublisher {
	return &MultiPublisher{pubs: pubs}
}

// Publish forwards f to each wrapped publisher. Non-blocking overall, since every wrapped publisher is
// required to be non-blocking itself (this is called on the ingest goroutine, single-producer, and
// must never stall it). nil entries are skipped defensively so a partially-constructed publisher list
// can't panic.
func (m *MultiPublisher) Publish(f *telemetryv1.TelemetryFrame) {
	for _, p := range m.pubs {
		if p == nil {
			continue
		}
		p.Publish(f)
	}
}
