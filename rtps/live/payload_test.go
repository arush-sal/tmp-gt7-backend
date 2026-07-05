package live

import (
	"encoding/json"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBuildPayload_Live(t *testing.T) {
	base := time.Unix(1700000000, 0)
	f := &telemetryv1.TelemetryFrame{
		CarSpeed: 214.5, CurrentGear: 4, Throttle: 1.0, Brake: 0, CurrentLap: 8,
		RecvTimestamp: timestamppb.New(base.Add(3 * time.Second)),
	}
	b, err := BuildPayload(f, base, true, Enrichment{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	for _, k := range []string{"served_at", "status", "frame", "derived", "enrichment"} {
		if _, ok := got[k]; !ok {
			t.Errorf("payload missing key %q", k)
		}
	}
	var frame map[string]any
	_ = json.Unmarshal(got["frame"], &frame)
	if frame["car_speed"] == nil || frame["current_gear"] == nil {
		t.Errorf("frame block missing direct metrics: %v", frame)
	}
	var derived map[string]any
	_ = json.Unmarshal(got["derived"], &derived)
	if derived["current_lap_time_ns"] == nil {
		t.Errorf("derived missing current_lap_time_ns: %v", derived)
	}
}

func TestBuildPayload_Idle(t *testing.T) {
	b, err := BuildPayload(nil, time.Time{}, false, Enrichment{})
	if err != nil {
		t.Fatalf("BuildPayload idle: %v", err)
	}
	var got map[string]json.RawMessage
	_ = json.Unmarshal(b, &got)
	var status string
	_ = json.Unmarshal(got["status"], &status)
	if status != "idle" {
		t.Errorf("status = %q, want idle", status)
	}
}
