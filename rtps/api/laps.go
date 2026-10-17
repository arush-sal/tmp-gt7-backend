package api

import (
	"net/http"
	"strconv"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"go.uber.org/zap"
)

// sectorSplitDTO is one sector's timing for a lap. Durations are int64 nanoseconds (time.Duration is
// int64 ns). DeltaToBestNs is a pointer so it serializes as JSON null (not 0) when the split has no
// timing-trustworthy pool (HasDelta == false).
type sectorSplitDTO struct {
	Index         int     `json:"index"`
	Label         string  `json:"label"`
	DurationNs    int64   `json:"duration_ns"`
	DeltaToBestNs *int64  `json:"delta_to_best_ns"`
	MinSpeed      float32 `json:"min_speed"`
}

// lapSectorsDTO is a lap's sector block: whether the splits are trustworthy geometry, the machine
// reason when not (null when reliable), and the splits.
type lapSectorsDTO struct {
	Reliable bool             `json:"reliable"`
	Reason   *string          `json:"reason"`
	Splits   []sectorSplitDTO `json:"splits"`
}

// lapDTO is the API view of one lap. DeltaToPBNs is a pointer so it serializes as null when the
// session has no PB (Record.BestLap <= 0).
type lapDTO struct {
	LapNumber   int32         `json:"lap_number"`
	DurationNs  int64         `json:"duration_ns"`
	Complete    bool          `json:"complete"`
	Valid       bool          `json:"valid"`
	Timed       bool          `json:"timed"`
	DeltaToPBNs *int64        `json:"delta_to_pb_ns"`
	Sectors     lapSectorsDTO `json:"sectors"`
}

// traceSampleDTO is one kept trace sample. TNs is nanoseconds since the lap's t0 (StartTime, or the
// first sample's RecvTime when the lap isn't found).
type traceSampleDTO struct {
	Seq        uint64  `json:"seq"`
	TNs        int64   `json:"t_ns"`
	Distance   float64 `json:"distance"`
	PosX       float32 `json:"pos_x"`
	PosZ       float32 `json:"pos_z"`
	Throttle   float32 `json:"throttle"`
	Brake      float32 `json:"brake"`
	Speed      float32 `json:"speed"`
	Gear       int32   `json:"gear"`
	RPM        float32 `json:"rpm"`
	Fuel       float32 `json:"fuel"`
	TyreTempFL float32 `json:"tyre_temp_fl"`
	TyreTempFR float32 `json:"tyre_temp_fr"`
	TyreTempRL float32 `json:"tyre_temp_rl"`
	TyreTempRR float32 `json:"tyre_temp_rr"`
	Clutch     float32 `json:"clutch"`
	Boost      float32 `json:"boost"`
	// SurfaceType is GT7's per-wheel surface code (4 chars FL,FR,RL,RR; T=tarmac C=kerb D=dirt G=grass
	// S=sand s=snow), empty for pre-format-C frames. Lets the client render kerb/grass ticks.
	SurfaceType string `json:"surface_type"`
	// Steering is the front-wheel steering angle in radians (mean of both front wheels; 0 when absent).
	Steering float32 `json:"steering"`
	// DeltaToBestNs is a pointer so it serializes JSON null (not 0) when the sample carried no delta.
	DeltaToBestNs *int64 `json:"delta_to_best_ns"`
}

// handleSessionLaps: GET /v1/sessions/{id}/laps -> every lap with its sector splits + deltas. Unlike
// /coaching this is NOT gated on session status (a live session's finished laps are served). delta_to_pb
// is against the stored Record.BestLap; null when there is no PB yet.
func (s *Server) handleSessionLaps(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	laps := s.laps.BySession(id) // nil/empty -> we still serve "[]"
	secs, err := processing.SessionLapSectors(r.Context(), s.sectors, s.traces, id, derefLaps(laps))
	if err != nil {
		s.log.Error("session lap sectors failed", zap.String("session", id), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to compute lap sectors")
		return
	}

	bySector := make(map[int32]processing.LapSectors, len(secs))
	for _, ls := range secs {
		bySector[ls.LapNumber] = ls
	}

	out := make([]lapDTO, 0, len(laps))
	for _, l := range laps {
		dto := lapDTO{
			LapNumber:  l.LapNumber,
			DurationNs: int64(l.Duration),
			Complete:   l.Complete,
			Valid:      l.Valid,
			Timed:      l.Timed,
		}
		if rec.BestLap > 0 {
			d := int64(l.Duration - rec.BestLap)
			dto.DeltaToPBNs = &d
		}
		dto.Sectors = sectorsDTO(bySector[l.LapNumber])
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// sectorsDTO converts a processing.LapSectors into its API block (reason null when reliable/empty,
// delta_to_best null when a split has no analyzable pool).
func sectorsDTO(ls processing.LapSectors) lapSectorsDTO {
	out := lapSectorsDTO{Reliable: ls.Reliable, Splits: make([]sectorSplitDTO, len(ls.Splits))}
	if ls.Reason != "" {
		reason := ls.Reason
		out.Reason = &reason
	}
	for i, sp := range ls.Splits {
		d := sectorSplitDTO{
			Index:      sp.Index,
			Label:      sp.Label,
			DurationNs: int64(sp.Duration),
			MinSpeed:   sp.MinSpeed,
		}
		if sp.HasDelta {
			v := int64(sp.DeltaToBest)
			d.DeltaToBestNs = &v
		}
		out.Splits[i] = d
	}
	return out
}

// handleLapTrace: GET /v1/sessions/{id}/laps/{n}/trace -> a lap's telemetry trace, decimated to at most
// ?decimate= points (default 1500), always preserving the first and last sample. t_ns is relative to
// the lap's StartTime when the lap is known, else the first sample's RecvTime.
func (s *Server) handleLapTrace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "invalid lap number")
		return
	}

	decimate := 1500
	if q := r.URL.Query().Get("decimate"); q != "" {
		if d, err := strconv.Atoi(q); err == nil && d > 0 {
			decimate = d
		}
	}

	samples, err := s.traces.ByLap(id, int32(n))
	if err != nil {
		s.log.Error("lap trace read failed", zap.String("session", id), zap.Int("lap", n), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to read lap trace")
		return
	}
	if len(samples) == 0 {
		writeJSON(w, http.StatusOK, []traceSampleDTO{})
		return
	}

	// t0: prefer the lap's StartTime when the lap exists, else the first sample's RecvTime.
	t0 := samples[0].RecvTime
	for _, l := range s.laps.BySession(id) {
		if l.LapNumber == int32(n) {
			t0 = l.StartTime
			break
		}
	}

	kept := decimateSamples(samples, decimate)
	out := make([]traceSampleDTO, len(kept))
	for i, sm := range kept {
		out[i] = traceSampleDTO{
			Seq:           sm.Seq,
			TNs:           sm.RecvTime.Sub(t0).Nanoseconds(),
			Distance:      sm.Distance,
			PosX:          sm.PosX,
			PosZ:          sm.PosZ,
			Throttle:      sm.Throttle,
			Brake:         sm.Brake,
			Speed:         sm.Speed,
			Gear:          sm.Gear,
			RPM:           sm.RPM,
			Fuel:          sm.Fuel,
			TyreTempFL:    sm.TyreTempFL,
			TyreTempFR:    sm.TyreTempFR,
			TyreTempRL:    sm.TyreTempRL,
			TyreTempRR:    sm.TyreTempRR,
			Clutch:        sm.Clutch,
			Boost:         sm.Boost,
			SurfaceType:   sm.SurfaceType,
			Steering:      sm.Steering,
			DeltaToBestNs: sm.DeltaToBestNs,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// decimateSamples keeps at most about `max` samples by striding, always including the first and last so
// the trace's endpoints (lap start/finish) are never dropped.
func decimateSamples(samples []processing.Sample, max int) []processing.Sample {
	if len(samples) <= max {
		return samples
	}
	stride := (len(samples) + max - 1) / max // ceil(len/max)
	out := make([]processing.Sample, 0, max+1)
	for i := 0; i < len(samples); i += stride {
		out = append(out, samples[i])
	}
	if last := len(samples) - 1; last%stride != 0 { // append the final sample if the stride skipped it
		out = append(out, samples[last])
	}
	return out
}

// derefLaps converts a []*Lap into a []Lap (SessionLapSectors takes values). Lap has no pointer
// fields, so a value copy is a full snapshot.
func derefLaps(laps []*processing.Lap) []processing.Lap {
	out := make([]processing.Lap, len(laps))
	for i, l := range laps {
		out[i] = *l
	}
	return out
}
