package tui

import (
	"encoding/json"
	"time"

	"github.com/arush-sal/garage/backend/rtps/analysis"
)

// LiveSnapshot is the dashboard-rendered SUBSET of the /v1/live payload — it decodes only the fields
// the TUI displays. Payload keys the dashboard doesn't use (served_at, and the reserved enrichment
// fields car_name/track/delta_to_best_ns) are intentionally omitted; extra JSON keys are ignored on
// unmarshal. Frame/Derived are nil in the idle state; Enrichment's fields are nil until they resolve
// (progressive enrichment).
type LiveSnapshot struct {
	Status     string         `json:"status"`
	Frame      *LiveFrame     `json:"frame"`
	Derived    *LiveDerived   `json:"derived"`
	Enrichment LiveEnrichment `json:"enrichment"`
}

// LiveFrame holds the canonical-frame fields the dashboard renders (snake_case, protojson). Lap-time
// fields arrive as protojson duration STRINGS ("117.842s") or null; use BestLap()/LastLap() to parse.
type LiveFrame struct {
	CarSpeed        float32 `json:"car_speed"`
	CurrentGear     uint32  `json:"current_gear"`
	SuggestedGear   uint32  `json:"suggested_gear"`
	Rpm             float32 `json:"rpm"`
	RpmRevWarning   uint32  `json:"rpm_rev_warning"`
	RpmRevLimiter   uint32  `json:"rpm_rev_limiter"`
	Throttle        float32 `json:"throttle"`
	Brake           float32 `json:"brake"`
	Clutch          float32 `json:"clutch"`
	Boost           float32 `json:"boost"`
	CurrentFuel     float32 `json:"current_fuel"`
	FuelCapacity    float32 `json:"fuel_capacity"`
	TyreTempFL      float32 `json:"tyre_temp_fl"`
	TyreTempFR      float32 `json:"tyre_temp_fr"`
	TyreTempRL      float32 `json:"tyre_temp_rl"`
	TyreTempRR      float32 `json:"tyre_temp_rr"`
	CurrentLap      int32   `json:"current_lap"`
	TotalLaps       int32   `json:"total_laps"`
	CurrentPosition int32   `json:"current_position"`
	TotalPositions  int32   `json:"total_positions"`
	BestLapStr      string  `json:"best_lap"`
	LastLapStr      string  `json:"last_lap"`
	InRace          bool    `json:"in_race"`
	IsPaused        bool    `json:"is_paused"`
}

// LiveDerived is the cheap per-frame computed block.
type LiveDerived struct {
	CurrentLapTimeNs int64   `json:"current_lap_time_ns"`
	FuelPct          float32 `json:"fuel_pct"`
	Source           string  `json:"source"`
}

// LiveEnrichment is the eventually-consistent correlated block; Analysis is nil until a lap completes.
type LiveEnrichment struct {
	SessionID string                    `json:"session_id"`
	Analysis  *analysis.SessionAnalysis `json:"analysis"`
}

// ParseSnapshot unmarshals one SSE event's JSON body into a LiveSnapshot.
func ParseSnapshot(data []byte) (LiveSnapshot, error) {
	var s LiveSnapshot
	err := json.Unmarshal(data, &s)
	return s, err
}

// BestLap parses the protojson duration string; nil frame or empty/invalid yields 0.
func (f *LiveFrame) BestLap() time.Duration { return parseProtoDur(f.bestLapStr()) }

// LastLap parses the protojson duration string; nil frame or empty/invalid yields 0.
func (f *LiveFrame) LastLap() time.Duration { return parseProtoDur(f.lastLapStr()) }

func (f *LiveFrame) bestLapStr() string {
	if f == nil {
		return ""
	}
	return f.BestLapStr
}

func (f *LiveFrame) lastLapStr() string {
	if f == nil {
		return ""
	}
	return f.LastLapStr
}

// parseProtoDur parses a protojson duration string ("117.842s"); empty/invalid yields 0.
func parseProtoDur(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s) // protojson emits Go-parseable "117.842s"
	if err != nil {
		return 0
	}
	return d
}
