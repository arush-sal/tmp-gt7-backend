package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/live"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// deltaFrame builds a minimal in-race frame for feeding a DeltaTracker directly (mirrors
// rtps/live/delta_test.go's dframe helper, which is unexported and in a different package).
func deltaFrame(x float32, ts time.Time, lap int32, last time.Duration) *telemetryv1.TelemetryFrame {
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

// liveEnvelope is the subset of the /v1/live JSON body this test cares about.
type liveEnvelope struct {
	Enrichment struct {
		DeltaToBestNs *int64 `json:"delta_to_best_ns"`
	} `json:"enrichment"`
}

// TestHandleLive_DeltaOverlay_Present feeds a DeltaTracker a reference lap plus a mapped probe frame
// (same pattern as rtps/live/delta_test.go's buildReferenceLap+probe), wires it into the Server, and
// asserts /v1/live surfaces the tracker's Delta() value as enrichment.delta_to_best_ns. This fails if
// handleLive's overlay (the `en.DeltaToBestNs = &ns` assignment in liveEnrichment) is removed.
func TestHandleLive_DeltaOverlay_Present(t *testing.T) {
	hub := live.New()
	hub.Publish(&telemetryv1.TelemetryFrame{RecvTimestamp: timestamppb.Now()})

	d := live.NewDeltaTracker()
	base := time.Unix(1_700_000_000, 0)
	// Reference lap 1: 11 points, 100m over 10s (10 m/s), then the lap-boundary frame that promotes it.
	for i := 0; i <= 10; i++ {
		d.Publish(deltaFrame(float32(i*10), base.Add(time.Duration(i)*time.Second), 1, 0))
	}
	start2 := base.Add(10 * time.Second)
	d.Publish(deltaFrame(0, start2, 2, 10*time.Second))
	// Probe frame on lap 2: faster (20 m/s), gives a non-null, negative delta.
	d.Publish(deltaFrame(50, start2.Add(2500*time.Millisecond), 2, 0))

	wantNs, ok := d.Delta()
	if !ok {
		t.Fatal("test setup: DeltaTracker.Delta() ok=false, want a mapped delta")
	}

	srv := NewServer(coach.Coach{}, nil, hub, live.NewEnrichmentState(), d, nil, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/live", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var body liveEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enrichment.DeltaToBestNs == nil {
		t.Fatal("enrichment.delta_to_best_ns = null, want non-null")
	}
	if *body.Enrichment.DeltaToBestNs != wantNs {
		t.Errorf("delta_to_best_ns = %d, want %d", *body.Enrichment.DeltaToBestNs, wantNs)
	}
}

// TestHandleLive_DeltaOverlay_NilTracker asserts that with no DeltaTracker wired (nil, the default when
// a caller has none), enrichment.delta_to_best_ns stays null rather than panicking or fabricating a
// value.
func TestHandleLive_DeltaOverlay_NilTracker(t *testing.T) {
	hub := live.New()
	hub.Publish(&telemetryv1.TelemetryFrame{RecvTimestamp: timestamppb.Now()})

	srv := NewServer(coach.Coach{}, nil, hub, live.NewEnrichmentState(), nil, nil, nil, nil, nil, nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/live", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var body liveEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enrichment.DeltaToBestNs != nil {
		t.Errorf("delta_to_best_ns = %v, want null with no DeltaTracker wired", *body.Enrichment.DeltaToBestNs)
	}
}
