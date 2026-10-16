// Package api is the API/Presentation domain (GARAGE domain 6). It holds ZERO racing intelligence
// (handoff principle): it serves the Coaching domain's reports over HTTP/JSON and exposes session
// metadata as small DTOs (not raw internal records).
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
	"github.com/arush-sal/garage/backend/web"
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

	// laps/traces/sectors back the per-session lap + trace + sector-timing routes (Task B2). Unlike
	// hub/enrich these are NOT replaced with safe empties: the routes require real stores, so a nil is
	// a wiring bug and is left to fault rather than silently serve empty data.
	laps    processing.LapStore
	traces  processing.TraceStore
	sectors processing.SectorProvider

	trackRecMu    sync.Mutex                 // guards trackRecCache
	trackRecCache map[string]json.RawMessage // slug -> marshalled rkb.Record; records are immutable at runtime, so this never invalidates
}

// NewServer wires the API server. Nil live dependencies are replaced with safe empties (a no-op
// logger, an empty Hub, an empty EnrichmentState) so the live handlers never nil-deref. The coach and
// session store are NOT guarded — the session routes require them and will fault if they are nil. delta
// is left nil when not supplied (rather than constructing a fresh DeltaTracker): a detached tracker
// never receives ingest frames, so it would silently and permanently report ok=false; nil is the
// honest "no delta overlay" state, and liveEnrichment skips it cleanly.
func NewServer(c coach.Coach, sessions session.Store, hub *live.Hub, enrich *live.EnrichmentState, delta *live.DeltaTracker, tracks rkb.Store, log *zap.Logger, laps processing.LapStore, traces processing.TraceStore, sectors processing.SectorProvider) *Server {
	if log == nil {
		log = zap.NewNop()
	}
	if hub == nil {
		hub = live.New()
	}
	if enrich == nil {
		enrich = live.NewEnrichmentState()
	}
	return &Server{coach: c, sessions: sessions, live: hub, enrich: enrich, delta: delta, tracks: tracks, log: log, laps: laps, traces: traces, sectors: sectors}
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
	mux.HandleFunc("GET /v1/sessions/{id}/laps", s.handleSessionLaps)
	mux.HandleFunc("GET /v1/sessions/{id}/laps/{n}/trace", s.handleLapTrace)
	mux.HandleFunc("GET /v1/sessions/{id}/laps/{n}/peaks", s.handleLapPeaks)
	mux.HandleFunc("GET /v1/sessions/{id}/median", s.handleSessionMedian)
	mux.HandleFunc("GET /v1/sessions/{id}/reference", s.handleSessionReference)
	mux.HandleFunc("GET /v1/live", s.handleLive)
	mux.HandleFunc("GET /v1/live/stream", s.handleLiveStream) // implemented in Task 5
	mux.HandleFunc("GET /v1/tracks", s.handleListTracks)
	// Literal "coverage" beats the "{slug}" wildcard by Go 1.22 mux precedence (most-specific wins).
	mux.HandleFunc("GET /v1/tracks/coverage", s.handleTracksCoverage)
	mux.HandleFunc("GET /v1/tracks/{slug}", s.handleTrack)
	// The embedded SPA owns everything else; the /v1/* routes above are more specific and win.
	mux.Handle("GET /", web.Handler())
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

	ctx := r.Context()

	// lastSlugSent is handler-goroutine-local: it tracks the last slug pushed as a "track" event on
	// THIS subscriber's connection, so a slug already resolved before connect still replays once
	// (initial connect below), and a slug that never changes never re-emits.
	var lastSlugSent *string

	// Emit the current snapshot immediately so a fresh client isn't blank until the next frame, and
	// replay the track event on connect (a client connecting after resolution gets it right away).
	s.emit(w, flusher)
	s.emitTrack(ctx, w, flusher, &lastSlugSent)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return // client disconnected
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": ping\n\n")) // comment frame keeps intermediaries open
			flusher.Flush()
			s.emitTrack(ctx, w, flusher, &lastSlugSent) // quiet-gap coverage (no frames flowing)
		case <-sub.C():
			if sub.Take() == nil {
				continue
			}
			s.emit(w, flusher)
			s.emitTrack(ctx, w, flusher, &lastSlugSent) // resolution mid-stream rides the next frame wake
		}
	}
}

// trackRecord returns the marshalled rkb.Record for slug, memoized on the Server (records are
// immutable at runtime once resolved, so the cache never needs invalidation). Returns ok=false when
// s.tracks is nil or the slug is unknown.
func (s *Server) trackRecord(ctx context.Context, slug string) (json.RawMessage, bool) {
	if s.tracks == nil {
		return nil, false
	}
	s.trackRecMu.Lock()
	if raw, ok := s.trackRecCache[slug]; ok {
		s.trackRecMu.Unlock()
		return raw, true
	}
	s.trackRecMu.Unlock()

	rec, found, err := s.tracks.Track(ctx, slug)
	if err != nil || !found {
		return nil, false
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return nil, false
	}

	s.trackRecMu.Lock()
	if s.trackRecCache == nil {
		s.trackRecCache = make(map[string]json.RawMessage)
	}
	s.trackRecCache[slug] = raw
	s.trackRecMu.Unlock()
	return raw, true
}

// trackEventPayload is the SSE "track" event body: the resolved slug (nil = cleared), its confidence,
// and the full RKB record so the frontend never has to fetch it separately.
type trackEventPayload struct {
	Slug       *string         `json:"slug"`
	Confidence *string         `json:"confidence,omitempty"`
	Record     json.RawMessage `json:"record,omitempty"`
}

// emitTrack pushes a "track" SSE event exactly when the resolved slug differs from *lastSlugSent
// (nil->slug, slug->slug2, slug->nil all count as a change); otherwise it is a no-op. This is what
// makes the full record a ONE-TIME push per (subscriber, slug) rather than riding every frame.
func (s *Server) emitTrack(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, lastSlugSent **string) {
	en := s.enrich.Get()
	cur := en.Track

	if !slugChanged(*lastSlugSent, cur) {
		return
	}

	payload := trackEventPayload{Confidence: en.TrackConfidence}
	if cur != nil {
		slug := *cur
		payload.Slug = &slug
		if raw, ok := s.trackRecord(ctx, slug); ok {
			payload.Record = raw
		} else {
			// Slug resolved but the RKB store has no record for it (or is unavailable): skip this
			// push rather than emit a track event with no record, and do not update lastSlugSent so
			// a later successful lookup still fires.
			s.log.Warn("track event skipped: no rkb record for resolved slug")
			return
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return // skip a bad payload rather than kill the stream
	}
	_, _ = w.Write([]byte("event: track\ndata: "))
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()

	*lastSlugSent = cur
}

// slugChanged reports whether cur differs from prev, treating nil as its own distinct value (so
// nil->slug, slug->nil, and slug->slug2 are all changes, but slug->slug is not).
func slugChanged(prev, cur *string) bool {
	if prev == nil && cur == nil {
		return false
	}
	if prev == nil || cur == nil {
		return true
	}
	return *prev != *cur
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
