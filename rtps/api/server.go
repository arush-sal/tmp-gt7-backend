// Package api is the API/Presentation domain (GARAGE domain 6). It holds ZERO racing intelligence
// (handoff principle): it serves the Coaching domain's reports over HTTP/JSON and exposes session
// metadata as small DTOs (not raw internal records).
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// Server serves the read API. It depends on the Coaching domain (for reports), the session store
// (for the session list), and the live egress Hub + EnrichmentState (for the /v1/live snapshot).
type Server struct {
	coach    coach.Coach
	sessions session.Store
	live     *live.Hub
	enrich   *live.EnrichmentState
	log      *zap.Logger
}

// NewServer wires the API server. A nil logger is replaced with a no-op.
func NewServer(c coach.Coach, sessions session.Store, hub *live.Hub, enrich *live.EnrichmentState, log *zap.Logger) *Server {
	if log == nil {
		log = zap.NewNop()
	}
	return &Server{coach: c, sessions: sessions, live: hub, enrich: enrich, log: log}
}

// Handler returns the HTTP routes (Go 1.22+ method+wildcard patterns; no router dependency).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions", s.handleListSessions)
	mux.HandleFunc("GET /v1/sessions/{id}/coaching", s.handleSessionCoaching)
	mux.HandleFunc("GET /v1/live", s.handleLive)
	mux.HandleFunc("GET /v1/live/stream", s.handleLiveStream) // implemented in Task 5
	mux.HandleFunc("GET /{$}", s.handleLive)                  // "/" alias, exact match only
	return mux
}

// sessionSummary is the API view of a session — identity + lifecycle, without internal fields like
// package ids.
type sessionSummary struct {
	ID         string    `json:"id"`
	CarID      int32     `json:"car_id"`
	Status     string    `json:"status"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	FrameCount uint64    `json:"frame_count"`
	GapCount   uint64    `json:"gap_count"`
	CurrentLap int32     `json:"current_lap"`
	TotalLaps  int32     `json:"total_laps"`
}

func summaryOf(r *session.Record) sessionSummary {
	return sessionSummary{
		ID: r.ID, CarID: r.CarID, Status: string(r.Status),
		StartTime: r.StartTime, EndTime: r.EndTime,
		FrameCount: r.FrameCount, GapCount: r.GapCount,
		CurrentLap: r.CurrentLap, TotalLaps: r.TotalLaps,
	}
}

// handleListSessions: GET /v1/sessions -> the session summaries in store order.
func (s *Server) handleListSessions(w http.ResponseWriter, _ *http.Request) {
	recs := s.sessions.List()
	out := make([]sessionSummary, 0, len(recs))
	for _, r := range recs {
		out = append(out, summaryOf(r))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSessionCoaching: GET /v1/sessions/{id}/coaching -> the coaching report (analysis + placeholder
// coaching) for one session.
func (s *Server) handleSessionCoaching(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.sessions.Get(id); !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	report, err := s.coach.SessionReport(r.Context(), id)
	if err != nil {
		s.log.Error("coaching report failed", zap.String("session", id), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to produce coaching report")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleLive: GET /v1/live (and / alias) -> the current frame + progressive-enrichment snapshot.
func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	frame, lapStart, ok := s.live.Latest()
	body, err := live.BuildPayload(frame, lapStart, ok, s.enrich.Get())
	if err != nil {
		s.log.Error("live payload encode failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to encode live payload")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleLiveStream is implemented in Task 5.
func (s *Server) handleLiveStream(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented")
}

// writeJSON marshals v to a buffer first, so an encode failure yields a clean 500 instead of a 200
// with a truncated body (headers/status are only written once encoding succeeds).
func writeJSON(w http.ResponseWriter, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"failed to encode response"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

// errorResponse is the stable error shape.
type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
