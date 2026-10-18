package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// recordingWait bounds how long POST /v1/live/recording waits for the ingest goroutine to apply the
// switch. Frames arrive at ~60 Hz while a session is open, so this is only hit when telemetry stalls.
const recordingWait = 2 * time.Second

// RecordingSwitch is the user's Record switch for the in-process telemetry stream
// (*session.RecordingControl). State reports (recording, open recorded session id); Set requests a
// value for the session expect names ("" = whichever is open) and returns the resulting state, or one
// of session.ErrNoSession, session.ErrSessionChanged, session.ErrStalled.
type RecordingSwitch interface {
	State() (recording bool, sessionID string)
	Set(on bool, expect string, wait time.Duration) (recording bool, sessionID string, err error)
}

// recordingRequest is the POST body. Recording is a pointer so a missing or null field is a 400.
// SessionID (optional, for older clients) is the session the UI shows; an off request applies only
// while that is still the open session (or the stored original the open one duplicates).
type recordingRequest struct {
	Recording *bool  `json:"recording"`
	SessionID string `json:"session_id"`
}

// recordingResponse is the POST 200 body.
type recordingResponse struct {
	Recording bool   `json:"recording"`
	SessionID string `json:"session_id"`
}

// handleSetRecording: POST /v1/live/recording {"recording": bool, "session_id"?: string}. Off discards
// the open session entirely; on starts a fresh session. 409 "no active session" when idle, 409
// "session changed" when session_id no longer names the open session, 503 when telemetry stalled (the
// request is withdrawn, nothing changes), 400 on a malformed body; setting the current value is a 200
// no-op. This is the API's only mutating route and it is unauthenticated:
// keep api.addr on localhost (the default) unless the network is trusted.
func (s *Server) handleSetRecording(w http.ResponseWriter, r *http.Request) {
	if s.noLiveFeed(w) {
		return
	}
	var req recordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Recording == nil {
		writeError(w, http.StatusBadRequest, `body must be {"recording": true|false}`)
		return
	}
	if s.Recording == nil {
		writeError(w, http.StatusConflict, "no active session")
		return
	}
	was, prevID := s.Recording.State()
	rec, id, err := s.Recording.Set(*req.Recording, req.SessionID, recordingWait)
	switch {
	case errors.Is(err, session.ErrStalled):
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	case errors.Is(err, session.ErrSessionChanged):
		writeError(w, http.StatusConflict, "session changed")
		return
	case err != nil:
		writeError(w, http.StatusConflict, "no active session")
		return
	}
	switch {
	case was && !rec:
		s.log.Info("recording disabled: session discarded", zap.String("session", prevID))
	case !was && rec:
		s.log.Info("recording enabled: new session", zap.String("session", id))
	}
	writeJSON(w, http.StatusOK, recordingResponse{Recording: rec, SessionID: id})
}
