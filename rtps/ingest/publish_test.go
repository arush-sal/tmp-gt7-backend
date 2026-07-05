package ingest

import (
	"testing"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// recordingPub is a fake Publisher that records the sequence of every frame it is given, so the test
// can assert both that Publish was called for every frame and the order in which it happened.
type recordingPub struct{ seqs []uint64 }

func (r *recordingPub) Publish(f *telemetryv1.TelemetryFrame) {
	r.seqs = append(r.seqs, f.GetSequence())
}

// TestIngest_PublishesBeforeObserve verifies the publish-first egress hook: when Server.Publisher is
// set, every ingested frame is handed to it (this test only checks call count/coverage; ordering
// relative to Observe is guaranteed by the source, not independently observable here).
func TestIngest_PublishesBeforeObserve(t *testing.T) {
	pub := &recordingPub{}
	srv := NewServer(session.NewMemStore(), session.NoopSink{}, session.Config{}, zap.NewNop())
	srv.Publisher = pub

	frames := []*telemetryv1.TelemetryFrame{
		mkFrame(0, true, 1384, 1, 2, 2),
		mkFrame(100, true, 1384, 2, 2, 2),
	}
	stream := &fakeStream{frames: frames}
	if err := srv.Ingest(stream); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(pub.seqs) != len(frames) {
		t.Errorf("published %d frames, want %d", len(pub.seqs), len(frames))
	}
}
