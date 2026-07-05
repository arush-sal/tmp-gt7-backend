package live

import (
	"testing"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BenchmarkHubPublishObserve(b *testing.B) {
	h := New()
	sub := h.Subscribe()
	defer sub.Close()
	f := &telemetryv1.TelemetryFrame{RecvTimestamp: timestamppb.Now()}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Publish(f)
		<-sub.C()
		_ = sub.Take()
	}
}
