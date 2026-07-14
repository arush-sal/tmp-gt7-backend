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
	"github.com/arush-sal/garage/backend/rtps/rkb"
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
	delta    *live.DeltaTracker
	tracks   rkb.Store // Reference-Knowledge RKB; nil when unavailable (routes then 503)
	log      *zap.Logger
}

// NewServer wires the API server. Nil live dependencies are replaced with safe empties (a no-op
// logger, an empty Hub, an empty EnrichmentState) so the live handlers never nil-deref. The coach and
// session store are NOT guarded — the session routes require them and will fault if they are nil. delta
// is left nil when not supplied (rather than constructing a fresh DeltaTracker): a detached tracker
// never receives ingest frames, so it would silently and permanently report ok=false; nil is the
// honest "no delta overlay" state, and liveEnrichment skips it cleanly.
func NewServer(c coach.Coach, sessions session.Store, hub *live.Hub, enrich *live.EnrichmentState, delta *live.DeltaTracker, tracks rkb.Store, log *zap.Logger) *Server {
	if log == nil {
		log = zap.NewNop()
	}
	if hub == nil {
		hub = live.New()
	}
	if enrich == nil {
		enrich = live.NewEnrichmentState()
	}
	return &Server{coach: c, sessions: sessions, live: hub, enrich: enrich, delta: delta, tracks: tracks, log: log}
}

// liveEnrichment returns the current enrichment snapshot with the live per-frame delta overlaid, when a
// DeltaTracker is wired. s.enrich.Get() returns a value copy, so setting DeltaToBestNs here mutates only
// that local copy, never the shared EnrichmentState (see Enrichment's immutability invariant).
func (s *Server) liveEnrichment() live.Enrichment {
	en := s.enrich.Get()
	if s.delta != nil {
		if ns, ok := s.delta.Delta(); ok {
			en.DeltaToBestNs = &ns
		}
	}
	return en
}

// Handler returns the HTTP routes (Go 1.22+ method+wildcard patterns; no router dependency).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions", s.handleListSessions)
	mux.HandleFunc("GET /v1/sessions/{id}/coaching", s.handleSessionCoaching)
	mux.HandleFunc("GET /v1/live", s.handleLive)
	mux.HandleFunc("GET /v1/live/stream", s.handleLiveStream) // implemented in Task 5
	mux.HandleFunc("GET /{$}", s.handleLive)                  // "/" alias, exact match only
	mux.HandleFunc("GET /v1/tracks", s.handleListTracks)
	mux.HandleFunc("GET /v1/tracks/{slug}", s.handleTrack)
	return mux
}

// sessionSummary is the API view of a session — identity + lifecycle, without internal fields like
// package ids.
type sessionSummary struct {
	ID         string    `json:"id"`
	CarID      int32     `json:"car_id"`
	CarName    string    `json:"car_name"`
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
		ID: r.ID, CarID: r.CarID, CarName: r.CarName, Status: string(r.Status),
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
// coaching) for one session. Gated to finished/abandoned sessions: an active session's stored
// Record.BestLap is frozen at confirm-time (attribute() only updates it while active), so serving a
// "coaching" report built from it would silently show a stale PB for a session still in progress. Live
// sessions have their own up-to-date view at /v1/live.
func (s *Server) handleSessionCoaching(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if rec.Status == session.StatusActive {
		writeError(w, http.StatusConflict, "session in progress; use /v1/live for the live view")
		return
	}
	report, err := s.coach.SessionReport(r.Context(), id, rec.BestLap) // rec.BestLap = GT7 authoritative (final for a finished session)
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
	body, err := live.BuildPayload(frame, lapStart, ok, s.liveEnrichment())
	if err != nil {
		s.log.Error("live payload encode failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to encode live payload")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleLiveStream: GET /v1/live/stream -> Server-Sent Events, one JSON payload per fresh frame.
// Event-driven (no ticker): the subscriber's latest-slot coalesces for slow clients. The network write
// happens here on the request goroutine, never under the Hub lock.
func (s *Server) handleLiveStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering
	w.WriteHeader(http.StatusOK)

	sub := s.live.Subscribe()
	defer sub.Close()

	// Emit the current snapshot immediately so a fresh client isn't blank until the next frame.
	s.emit(w, flusher)

	ctx := r.Context()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return // client disconnected
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n")) // comment frame keeps intermediaries open
			flusher.Flush()
		case <-sub.C():
			if sub.Take() == nil {
				continue
			}
			s.emit(w, flusher)
		}
	}
}

// emit writes the current live payload as one SSE data event.
func (s *Server) emit(w http.ResponseWriter, flusher http.Flusher) {
	frame, lapStart, ok := s.live.Latest()
	body, err := live.BuildPayload(frame, lapStart, ok, s.liveEnrichment())
	if err != nil {
		return // skip a bad frame rather than kill the stream
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}

// handleListTracks: GET /v1/tracks -> the RKB index (track picker + tier).
func (s *Server) handleListTracks(w http.ResponseWriter, r *http.Request) {
	if s.tracks == nil {
		writeError(w, http.StatusServiceUnavailable, "track RKB unavailable")
		return
	}
	idx, err := s.tracks.Index(r.Context())
	if err != nil {
		s.log.Error("rkb index failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list tracks")
		return
	}
	writeJSON(w, http.StatusOK, idx)
}

// handleTrack: GET /v1/tracks/{slug} -> the full assembled RKB record; 404 for an unknown slug.
func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	if s.tracks == nil {
		writeError(w, http.StatusServiceUnavailable, "track RKB unavailable")
		return
	}
	rec, found, err := s.tracks.Track(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.log.Error("rkb track failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to load track")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "track not found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
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
