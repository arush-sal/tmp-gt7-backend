package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/arush-sal/garage/backend/rtps/processing"
	"go.uber.org/zap"
)

// handleSessionReference: GET /v1/sessions/{id}/reference -> the per-channel median line across the
// session's full racing laps, on a shared distance-fraction grid. ?channels= is a comma-separated
// filter (absent/empty -> all continuous channels); ?points= sets the grid resolution (default 1000,
// clamped to [2,5000] by MedianReference).
func (s *Server) handleSessionReference(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.sessions.Get(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var channels []string
	if q := r.URL.Query().Get("channels"); q != "" {
		channels = strings.Split(q, ",")
	}

	points := 1000
	if q := r.URL.Query().Get("points"); q != "" {
		if p, err := strconv.Atoi(q); err == nil && p > 0 {
			points = p
		}
	}

	res, err := processing.MedianReference(s.traces, id, derefLaps(s.laps.BySession(id)), channels, points)
	if err != nil {
		s.log.Error("session reference failed", zap.String("session", id), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to compute reference")
		return
	}
	writeJSON(w, http.StatusOK, res)
}
