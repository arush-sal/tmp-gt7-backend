package api

import (
	"math"
	"net/http"
	"strconv"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"go.uber.org/zap"
)

// extremumDTO is one speed peak/valley on the 0..1 track-distance axis, annotated with its time delta to
// the resolved reference. DeltaNs is a pointer so it serializes JSON null (not 0) when the reference is
// not usable (reference_reliable false).
type extremumDTO struct {
	Index    int     `json:"index"`
	Kind     string  `json:"kind"`     // "peak" | "valley"
	Fraction float64 `json:"fraction"` // 0..1 track position
	Distance float64 `json:"distance"` // cumulative in-lap metres
	Speed    float32 `json:"speed"`    // km/h, raw (un-smoothed)
	DeltaNs  *int64  `json:"delta_ns"` // ns vs reference at Fraction (+ == this lap behind); null when reference unusable
}

// referenceDTO echoes WHICH reference the deltas were measured against, so the client can label the
// overlay. LapNumber is set only for reference B (kind == "lap").
type referenceDTO struct {
	Kind      string `json:"kind"`       // "predicted" | "lap"
	LapNumber *int32 `json:"lap_number"` // null unless kind == "lap"
}

// peaksDTO is the /peaks response. Two independent trust axes tell the client WHY a delta may be null:
// the queried lap's own trace trust (reliable/reason) and the reference's usability
// (reference_reliable/reference_reason). Both use the pointer-reason convention of lapSectorsDTO (null
// when trustworthy).
type peaksDTO struct {
	Reliable          bool          `json:"reliable"`           // queried lap trace trust (SectorAnalyzable)
	Reason            *string       `json:"reason"`             // null when reliable
	Reference         referenceDTO  `json:"reference"`          // which reference these deltas use
	ReferenceReliable bool          `json:"reference_reliable"` // reference usable -> delta_ns populated
	ReferenceReason   *string       `json:"reference_reason"`   // null when reference_reliable
	Points            []extremumDTO `json:"points"`
}

// lapByNumber returns the session's lap with the given number, or nil if the session has no such lap.
// TraceStore.ByLap cannot distinguish "unknown lap" from "empty lap", so lap existence (and the
// Duration that SectorAnalyzable needs) is established against the LapStore instead. Shared by the
// queried-lap and the ?reference=<n> lookups.
func (s *Server) lapByNumber(id string, n int32) *processing.Lap {
	for _, l := range s.laps.BySession(id) {
		if l.LapNumber == n {
			return l
		}
	}
	return nil
}

// handleLapPeaks: GET /v1/sessions/{id}/laps/{n}/peaks?reference= -> the significant speed peaks &
// valleys of one lap on the 0..1 track-distance axis, each annotated with its time delta to the chosen
// reference (predicted-best by default, or a specific lap via ?reference=<n>). It 404s a missing
// session / lap / trace / named reference lap (no silent empties); a present-but-low-trust lap or an
// unusable reference returns 200 with the respective reliable=false + reason (and null delta_ns)
// rather than hiding it.
//
// Note the deliberate reference-A/B asymmetry on infra failure: a per-lap trace read error during the
// predicted-best build is swallowed by PredictedReferenceLap (that lap is excluded, as when it simply
// doesn't qualify), so a storage outage degrades reference A to reference_reliable:false rather than
// 500 — matching what /reference already ships. A named reference-B lap's read error is a hard 500,
// because the client asked for that specific resource.
func (s *Server) handleLapPeaks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.sessions.Get(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "invalid lap number")
		return
	}

	lap := s.lapByNumber(id, int32(n))
	if lap == nil {
		writeError(w, http.StatusNotFound, "lap not found")
		return
	}
	samples, err := s.traces.ByLap(id, int32(n))
	if err != nil {
		s.log.Error("lap peaks read failed", zap.String("session", id), zap.Int("lap", n), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to read lap trace")
		return
	}
	if len(samples) == 0 {
		writeError(w, http.StatusNotFound, "lap trace not found")
		return
	}

	qn := processing.Normalize(samples)
	ex := processing.SpeedExtrema(qn, processing.DefaultExtremaConfig())

	refFn, refDTO, refReliable, refReason, handled := s.resolveReference(w, r, id, qn)
	if handled {
		return
	}
	if refFn != nil {
		processing.AnnotateReferenceDelta(&ex, qn, refFn)
	}

	out := peaksDTO{
		Reliable:          processing.SectorAnalyzable(lap.Duration, samples),
		Reference:         refDTO,
		ReferenceReliable: refReliable,
		Points:            make([]extremumDTO, len(ex.Points)),
	}
	if !out.Reliable {
		reason := "lap trace does not faithfully cover the lap (span vs duration out of tolerance)"
		out.Reason = &reason
	}
	if !refReliable {
		out.ReferenceReason = &refReason
	}
	for i, e := range ex.Points {
		out.Points[i] = extremumDTO{
			Index:    e.Index,
			Kind:     e.Kind.String(),
			Fraction: e.Fraction,
			Distance: e.Distance,
			Speed:    e.Speed,
			DeltaNs:  e.DeltaNs,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// resolveReference builds the delta reference from ?reference=. Returns (refFn, descriptor, reliable,
// reason, handled). handled == true means it already wrote a terminal 4xx/5xx (malformed param, unknown
// reference lap, reference trace read error) and the caller must return immediately. When reliable ==
// false (a present-but-untrusted reference) refFn is nil and reason is set, so deltas stay null without
// failing the request.
//
// Grammar: absent, empty, or "predicted" -> reference A (predicted-best). Otherwise the value must parse
// as a non-negative int within int32 range -> reference B for that lap number. Anything else -> 400.
func (s *Server) resolveReference(w http.ResponseWriter, r *http.Request, id string, queried processing.NormalizedLap) (
	refFn processing.RefElapsedFn, dto referenceDTO, reliable bool, reason string, handled bool) {

	q := r.URL.Query().Get("reference")

	// --- A: predicted-best (default / explicit "predicted") ---
	if q == "" || q == "predicted" {
		dto = referenceDTO{Kind: "predicted"}
		ref, err := processing.PredictedReferenceLap(r.Context(), s.sectors, s.traces, id, derefLaps(s.laps.BySession(id)))
		if err != nil {
			s.log.Error("lap peaks reference failed", zap.String("session", id), zap.Error(err))
			writeError(w, http.StatusInternalServerError, "failed to build reference lap")
			return nil, dto, false, "", true
		}
		// An empty pool can still report Reliable==true, so gate on both.
		if !ref.Reliable || len(ref.Samples) == 0 {
			reason = "no reliable predicted reference lap for this session yet"
			if ref.Reason != "" {
				reason = ref.Reason
			}
			return nil, dto, false, reason, false
		}
		return processing.PredictedElapsed(ref), dto, true, "", false
	}

	// --- B: a specific recorded lap ---
	// Bound-check before the int32 cast: a value > MaxInt32 would silently wrap.
	rn, err := strconv.Atoi(q)
	if err != nil || rn < 0 || rn > math.MaxInt32 {
		writeError(w, http.StatusBadRequest, "invalid reference")
		return nil, dto, false, "", true
	}
	rn32 := int32(rn)
	dto = referenceDTO{Kind: "lap", LapNumber: &rn32}
	refLap := s.lapByNumber(id, rn32)
	if refLap == nil {
		writeError(w, http.StatusNotFound, "reference lap not found")
		return nil, dto, false, "", true
	}
	refSamples, err := s.traces.ByLap(id, rn32)
	if err != nil {
		s.log.Error("reference lap trace read failed", zap.String("session", id), zap.Int("lap", rn), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to read reference lap trace")
		return nil, dto, false, "", true
	}
	if len(refSamples) == 0 || !processing.SectorAnalyzable(refLap.Duration, refSamples) {
		return nil, dto, false, "reference lap has no analyzable trace", false
	}
	return processing.LapElapsed(processing.Normalize(refSamples)), dto, true, "", false
}
