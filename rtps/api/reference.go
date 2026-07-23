package api

import (
	"net/http"
	"strconv"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"go.uber.org/zap"
)

// predictedSectorDTO is one sector of the predicted lap: which pooled lap owns it and how long that
// owner's sector took (nanoseconds).
type predictedSectorDTO struct {
	Index      int    `json:"index"`
	Label      string `json:"label"`
	OwnerLap   int32  `json:"owner_lap"`
	DurationNs int64  `json:"duration_ns"`
}

// predictedSampleDTO is one spliced telemetry point. TNs is elapsed time in the synthetic lap; Distance
// is the re-accumulated spliced distance. DeltaToBestNs is a pointer so it serializes JSON null (not 0)
// when the owner sample carried no delta.
type predictedSampleDTO struct {
	Fraction      float64 `json:"fraction"`
	TNs           int64   `json:"t_ns"`
	Distance      float64 `json:"distance"`
	PosX          float32 `json:"pos_x"`
	PosZ          float32 `json:"pos_z"`
	Speed         float32 `json:"speed"`
	Throttle      float32 `json:"throttle"`
	Brake         float32 `json:"brake"`
	RPM           float32 `json:"rpm"`
	Gear          int32   `json:"gear"`
	Fuel          float32 `json:"fuel"`
	TyreTempFL    float32 `json:"tyre_temp_fl"`
	TyreTempFR    float32 `json:"tyre_temp_fr"`
	TyreTempRL    float32 `json:"tyre_temp_rl"`
	TyreTempRR    float32 `json:"tyre_temp_rr"`
	Clutch        float32 `json:"clutch"`
	Boost         float32 `json:"boost"`
	DeltaToBestNs *int64  `json:"delta_to_best_ns"`
}

// predictedLapDTO is the API view of a predicted (ideal) lap. Reason is null when the splice used real
// RKB geometry (Reliable true).
type predictedLapDTO struct {
	Reliable        bool                 `json:"reliable"`
	Reason          *string              `json:"reason"`
	PredictedBestNs int64                `json:"predicted_best_ns"`
	Sectors         []predictedSectorDTO `json:"sectors"`
	Samples         []predictedSampleDTO `json:"samples"`
}

// handleSessionReference: GET /v1/sessions/{id}/reference -> the predicted (ideal) lap: the fastest
// sector segments the driver has run, spliced into one synthetic lap's full telemetry trace. ?decimate=
// caps the samples array (default 1500, always preserving the first + last sample), mirroring /trace.
// The per-channel median line moved to /v1/sessions/{id}/median.
func (s *Server) handleSessionReference(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.sessions.Get(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	decimate := 1500
	if q := r.URL.Query().Get("decimate"); q != "" {
		if d, err := strconv.Atoi(q); err == nil && d > 0 {
			decimate = d
		}
	}

	pl, err := processing.PredictedReferenceLap(r.Context(), s.sectors, s.traces, id, derefLaps(s.laps.BySession(id)))
	if err != nil {
		s.log.Error("session reference failed", zap.String("session", id), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to compute reference")
		return
	}
	writeJSON(w, http.StatusOK, toPredictedLapDTO(pl, decimate))
}

// toPredictedLapDTO maps a processing.PredictedLap to its API DTO, decimating the samples array to at
// most `decimate` points (first + last always kept).
func toPredictedLapDTO(pl processing.PredictedLap, decimate int) predictedLapDTO {
	out := predictedLapDTO{
		Reliable:        pl.Reliable,
		PredictedBestNs: pl.PredictedBestNs,
		Sectors:         make([]predictedSectorDTO, len(pl.Sectors)),
	}
	if pl.Reason != "" {
		reason := pl.Reason
		out.Reason = &reason
	}
	for i, sec := range pl.Sectors {
		out.Sectors[i] = predictedSectorDTO{Index: sec.Index, Label: sec.Label, OwnerLap: sec.OwnerLap, DurationNs: sec.DurationNs}
	}
	kept := decimatePredicted(pl.Samples, decimate)
	out.Samples = make([]predictedSampleDTO, len(kept))
	for i, sm := range kept {
		out.Samples[i] = predictedSampleDTO{
			Fraction:      sm.Fraction,
			TNs:           sm.TNs,
			Distance:      sm.Distance,
			PosX:          sm.PosX,
			PosZ:          sm.PosZ,
			Speed:         sm.Speed,
			Throttle:      sm.Throttle,
			Brake:         sm.Brake,
			RPM:           sm.RPM,
			Gear:          sm.Gear,
			Fuel:          sm.Fuel,
			TyreTempFL:    sm.TyreTempFL,
			TyreTempFR:    sm.TyreTempFR,
			TyreTempRL:    sm.TyreTempRL,
			TyreTempRR:    sm.TyreTempRR,
			Clutch:        sm.Clutch,
			Boost:         sm.Boost,
			DeltaToBestNs: sm.DeltaToBestNs,
		}
	}
	return out
}

// decimatePredicted strides a predicted lap's samples down to at most `max` points, always keeping the
// first and last. It mirrors decimateSamples (laps.go) but for []processing.PredictedSample — laps.go's
// helper is typed to processing.Sample and that file is off-limits to generalize.
func decimatePredicted(samples []processing.PredictedSample, max int) []processing.PredictedSample {
	if len(samples) <= max {
		return samples
	}
	stride := (len(samples) + max - 1) / max // ceil(len/max)
	out := make([]processing.PredictedSample, 0, max+1)
	for i := 0; i < len(samples); i += stride {
		out = append(out, samples[i])
	}
	if last := len(samples) - 1; last%stride != 0 { // append the final sample if the stride skipped it
		out = append(out, samples[last])
	}
	return out
}
