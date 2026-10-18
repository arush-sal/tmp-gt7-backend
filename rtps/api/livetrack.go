package api

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

// TrackPicker is the user's track pick for the live session (*processing.TrackOverride). Set picks a
// slug for the most recently started open session, applied before it returns (appliesTo "current",
// its id), or, with none open, the next one ("next", ""); Clear withdraws it; Pending is a pick waiting for the next session; UserTrack is the published
// track when it came from a pick ("" otherwise).
type TrackPicker interface {
	Set(slug string) (appliesTo, sessionID string)
	Clear()
	Pending() string
	UserTrack() string
}

// liveTrackRequest is the POST body. TrackSlug is a pointer so a missing or null field is a 400.
type liveTrackRequest struct {
	TrackSlug *string `json:"track_slug"`
}

// liveTrackResponse is the POST 200 body (and, with TrackSlug nil, the DELETE one).
type liveTrackResponse struct {
	TrackSlug *string `json:"track_slug"`
	AppliesTo string  `json:"applies_to,omitempty"`
	SessionID *string `json:"session_id,omitempty"`
}

// handleSetLiveTrack: POST /v1/live/track {"track_slug": string}. The user's "this is <track>": it
// applies to the open session at once, or to the next session when none is open, wins over detection
// and is cleared when that session ends. 404 for a slug not in the RKB, 400 on a bad body, 503 when
// the RKB or the picker is not wired.
func (s *Server) handleSetLiveTrack(w http.ResponseWriter, r *http.Request) {
	if s.noLiveFeed(w) {
		return
	}
	var req liveTrackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TrackSlug == nil || *req.TrackSlug == "" {
		writeError(w, http.StatusBadRequest, `body must be {"track_slug": "<slug>"}`)
		return
	}
	if s.tracks == nil || s.TrackPick == nil {
		writeError(w, http.StatusServiceUnavailable, "track override unavailable")
		return
	}
	slug := *req.TrackSlug
	if _, found, err := s.tracks.Track(r.Context(), slug); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up track")
		return
	} else if !found {
		writeError(w, http.StatusNotFound, "unknown track")
		return
	}
	applies, id := s.TrackPick.Set(slug)
	s.log.Info("track picked by the user", zap.String("slug", slug), zap.String("applies_to", applies), zap.String("session", id))
	writeJSON(w, http.StatusOK, liveTrackResponse{TrackSlug: &slug, AppliesTo: applies, SessionID: &id})
}

// handleClearLiveTrack: DELETE /v1/live/track clears the pick (applied or pending); detection resumes.
func (s *Server) handleClearLiveTrack(w http.ResponseWriter, _ *http.Request) {
	if s.noLiveFeed(w) {
		return
	}
	if s.TrackPick != nil {
		s.TrackPick.Clear()
		s.log.Info("user track pick cleared")
	}
	writeJSON(w, http.StatusOK, liveTrackResponse{})
}
