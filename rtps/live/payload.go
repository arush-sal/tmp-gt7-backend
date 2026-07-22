package live

import (
	"encoding/json"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// derived is the cheap per-frame computed block (no correlation needed).
type derived struct {
	CurrentLapTimeNs int64   `json:"current_lap_time_ns"`
	FuelPct          float32 `json:"fuel_pct"`
	Source           string  `json:"source"`
	// LapNumber is the current 1-based flying lap (GT7 current_lap), or null on the formation lap (0),
	// the post-race cool-down lap (> total_laps), and when idle. The frontend keys completed-lap
	// selection to /laps lap_number, which uses the same GT7 current_lap convention.
	LapNumber *int32 `json:"lap_number"`
}

// livePayload is the wire shape. Frame + Enrichment are pre-marshaled RawMessage so the frame can use
// protojson (all fields, snake_case) while the wrapper uses encoding/json.
type livePayload struct {
	ServedAt   string          `json:"served_at"`
	Status     string          `json:"status"`
	Frame      json.RawMessage `json:"frame"`
	Derived    *derived        `json:"derived"`
	Enrichment json.RawMessage `json:"enrichment"`
}

// protoMarshal emits every frame field (EmitUnpopulated) in snake_case (UseProtoNames), so the payload
// stays in lockstep with the schema without hand-mapping.
var protoMarshal = protojson.MarshalOptions{EmitUnpopulated: true, UseProtoNames: true}

// BuildPayload assembles the full live snapshot JSON. When ok is false (no frame yet) it returns an
// idle payload with a null frame. Precondition: when ok is true the caller must pass a non-nil frame
// with its RecvTimestamp set and a valid lapStart. Hub.Latest returns these unvalidated, so the
// guarantee actually rests on every Hub.Publish caller (today the ingest path, which always publishes
// a canonical frame with RecvTimestamp set); otherwise the derived current_lap_time_ns is computed from
// a zero/garbage instant and may be a large or nonsensical value.
func BuildPayload(frame *telemetryv1.TelemetryFrame, lapStart time.Time, ok bool, en Enrichment) ([]byte, error) {
	enrJSON, err := json.Marshal(en)
	if err != nil {
		return nil, err
	}
	p := livePayload{
		ServedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Status:     "idle",
		Frame:      json.RawMessage("null"),
		Enrichment: enrJSON,
	}
	if ok && frame != nil {
		fb, err := protoMarshal.Marshal(frame)
		if err != nil {
			return nil, err
		}
		p.Status = "live"
		p.Frame = fb
		d := &derived{
			CurrentLapTimeNs: frame.GetRecvTimestamp().AsTime().Sub(lapStart).Nanoseconds(),
			FuelPct:          fuelPct(frame),
			Source:           frame.GetSource().String(),
		}
		// Expose a stable 1-based flying lap. Skip the formation lap (cl == 0) and the post-race
		// cool-down lap (cl > tl). The tl <= 0 guard keeps practice/time-trial (no lap target) working:
		// any flying lap counts. Copy to a local before taking its address to avoid aliasing.
		cl := frame.GetCurrentLap()
		tl := frame.GetTotalLaps()
		if cl > 0 && (tl <= 0 || cl <= tl) {
			n := cl
			d.LapNumber = &n
		}
		p.Derived = d
	}
	return json.Marshal(p)
}

// fuelPct returns current fuel as a percentage of tank capacity, or 0 when capacity is unknown/zero
// (e.g. some cars report no fuel data) to avoid a division by zero.
func fuelPct(f *telemetryv1.TelemetryFrame) float32 {
	capacity := f.GetFuelCapacity()
	if capacity <= 0 {
		return 0
	}
	return f.GetCurrentFuel() / capacity * 100
}
