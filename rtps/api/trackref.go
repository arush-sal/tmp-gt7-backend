package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"go.uber.org/zap"
)

// carDTO identifies the car a stored lap was driven in; name/category are null when unknown.
type carDTO struct {
	ID       int32   `json:"id"`
	Name     *string `json:"name"`
	Category *string `json:"category"`
}

// trackReferenceDTO is the GET/PUT/DELETE /v1/tracks/{slug}/reference-lap body: the predicted-lap
// (ReferenceDTO) fields for the one reference lap, plus where the lap came from.
type trackReferenceDTO struct {
	predictedLapDTO
	Source     string    `json:"source"` // "pinned" | "fastest"
	SessionID  string    `json:"session_id"`
	LapNumber  int32     `json:"lap_number"`
	LapTime    *string   `json:"lap_time"`
	RecordedAt time.Time `json:"recorded_at"` // the lap's start time
	Car        carDTO    `json:"car"`
}

// trackLapDTO is one row of GET /v1/tracks/{slug}/laps.
type trackLapDTO struct {
	SessionID  string    `json:"session_id"`
	LapNumber  int32     `json:"lap_number"`
	LapTime    *string   `json:"lap_time"`
	RecordedAt time.Time `json:"recorded_at"`
	Car        carDTO    `json:"car"`
	Pinned     bool      `json:"pinned"`
}

// pinRequest is the PUT body. Pointers make a missing field a 400.
type pinRequest struct {
	SessionID *string `json:"session_id"`
	LapNumber *int32  `json:"lap_number"`
}

// maxTrackLaps caps GET /v1/tracks/{slug}/laps?limit= (each row costs a session lookup).
const maxTrackLaps = 200

// carOf returns the car of a stored lap's session (zero id with null fields when the session is gone).
func (s *Server) carOf(sessionID string) carDTO {
	rec, ok := s.sessions.Get(sessionID)
	if !ok {
		return carDTO{}
	}
	return carDTO{ID: rec.CarID, Name: nonEmpty(rec.CarName), Category: nonEmpty(rec.CarCategory)}
}

// nonEmpty returns &v, or nil for "".
func nonEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// writeTrackReference resolves slug's reference lap and writes it (200), or 404 when there is none.
func (s *Server) writeTrackReference(w http.ResponseWriter, r *http.Request, slug string) {
	if s.TrackRefs == nil {
		writeError(w, http.StatusNotFound, "no stored reference for track")
		return
	}
	ref, ok, err := processing.ResolveTrackReference(s.TrackRefs, slug, s.log)
	if err != nil {
		s.log.Error("resolve track reference failed", zap.String("slug", slug), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to resolve reference lap")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no stored reference for track")
		return
	}
	decimate := 1500
	if q := r.URL.Query().Get("decimate"); q != "" {
		if d, err := strconv.Atoi(q); err == nil && d > 0 {
			decimate = d
		}
	}
	pl, err := processing.TrackReferenceLap(r.Context(), s.sectors, s.traces, ref.Lap)
	if err != nil {
		s.log.Error("track reference lap failed", zap.String("slug", slug), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to compute reference")
		return
	}
	writeJSON(w, http.StatusOK, trackReferenceDTO{
		predictedLapDTO: toPredictedLapDTO(pl, decimate),
		Source:          ref.Source,
		SessionID:       ref.Lap.SessionID,
		LapNumber:       ref.Lap.LapNumber,
		LapTime:         protoDuration(ref.Lap.Duration),
		RecordedAt:      ref.Lap.StartTime,
		Car:             s.carOf(ref.Lap.SessionID),
	})
}

// handleGetTrackReference: GET /v1/tracks/{slug}/reference-lap?decimate=N -> the track's stored
// reference lap (pinned, else the fastest confident complete valid lap on exactly this slug, any car).
func (s *Server) handleGetTrackReference(w http.ResponseWriter, r *http.Request) {
	s.writeTrackReference(w, r, r.PathValue("slug"))
}

// handlePinTrackReference: PUT /v1/tracks/{slug}/reference-lap {"session_id", "lap_number"} pins a
// stored lap as the track's reference: 400 bad body, 404 no such lap, 409 when the lap is not
// complete+valid on exactly this track with confident detection; 200 with the GET body.
func (s *Server) handlePinTrackReference(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var req pinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == nil || *req.SessionID == "" || req.LapNumber == nil {
		writeError(w, http.StatusBadRequest, `body must be {"session_id": string, "lap_number": int}`)
		return
	}
	if s.TrackRefs == nil {
		writeError(w, http.StatusNotFound, "lap not found")
		return
	}
	lap, ok, err := s.TrackRefs.Lap(*req.SessionID, *req.LapNumber)
	if err != nil {
		s.log.Error("lap lookup failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to read lap")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "lap not found")
		return
	}
	if !processing.RefEligible(lap, slug) {
		writeError(w, http.StatusConflict, "lap not usable as reference")
		return
	}
	err = s.TrackRefs.PinReference(slug, lap.SessionID, lap.LapNumber)
	if errors.Is(err, processing.ErrLapNotFound) { // deleted between the lookup and the pin
		writeError(w, http.StatusNotFound, "lap not found")
		return
	}
	if err != nil {
		s.log.Error("pin reference failed", zap.String("slug", slug), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to pin reference lap")
		return
	}
	s.log.Info("reference lap pinned", zap.String("slug", slug), zap.String("session", lap.SessionID), zap.Int32("lap", lap.LapNumber))
	s.referenceChanged(slug)
	s.writeTrackReference(w, r, slug)
}

// handleUnpinTrackReference: DELETE /v1/tracks/{slug}/reference-lap clears the pin; 200 with the
// fallback (the GET body, source "fastest"), or 404 when the track has no stored reference left.
func (s *Server) handleUnpinTrackReference(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if s.TrackRefs != nil {
		removed, err := s.TrackRefs.UnpinReference(slug)
		if err != nil {
			s.log.Error("unpin reference failed", zap.String("slug", slug), zap.Error(err))
			writeError(w, http.StatusInternalServerError, "failed to clear reference lap")
			return
		}
		if removed {
			s.log.Info("reference lap unpinned", zap.String("slug", slug))
			s.referenceChanged(slug)
		}
	}
	s.writeTrackReference(w, r, slug)
}

// referenceChanged tells the live stored-reference loader that slug's reference may have changed.
func (s *Server) referenceChanged(slug string) {
	if s.OnReferenceChange != nil {
		s.OnReferenceChange(slug)
	}
}

// handleTrackLaps: GET /v1/tracks/{slug}/laps?limit=N (default 20, max maxTrackLaps) -> the track's
// stored reference-eligible laps, fastest first, across sessions and cars, flagging the pinned one.
// The pinned lap is always included (appended last when it is outside the top N).
func (s *Server) handleTrackLaps(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	out := []trackLapDTO{}
	if s.TrackRefs == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	limit := 20
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = min(n, maxTrackLaps)
		}
	}
	laps, err := s.TrackRefs.TrackLaps(slug, limit)
	if err != nil {
		s.log.Error("track laps failed", zap.String("slug", slug), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list track laps")
		return
	}
	pinned, hasPin, err := s.TrackRefs.PinnedReference(slug)
	if err != nil {
		s.log.Error("pinned reference lookup failed", zap.String("slug", slug), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list track laps")
		return
	}
	isPin := func(l *processing.Lap) bool {
		return hasPin && pinned.SessionID == l.SessionID && pinned.LapNumber == l.LapNumber
	}
	if hasPin && !pinned.RaceLap1() && !slices.ContainsFunc(laps, isPin) {
		laps = append(laps, pinned)
	}
	cars := map[string]carDTO{} // one session lookup per session, not per row
	for _, l := range laps {
		car, ok := cars[l.SessionID]
		if !ok {
			car = s.carOf(l.SessionID)
			cars[l.SessionID] = car
		}
		out = append(out, trackLapDTO{
			SessionID: l.SessionID, LapNumber: l.LapNumber, LapTime: protoDuration(l.Duration),
			RecordedAt: l.StartTime, Car: car, Pinned: isPin(l),
		})
	}
	writeJSON(w, http.StatusOK, out)
}
