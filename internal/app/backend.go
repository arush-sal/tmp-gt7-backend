package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	cingest "github.com/arush-sal/garage/backend/collector/ingest"
	"github.com/arush-sal/garage/backend/collector/replay"
	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/internal/config"
	"github.com/arush-sal/garage/backend/internal/db"
	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/api"
	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/ingest"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/rkbref"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// RunBackend starts the rtps process: the in-process telemetry source (the GT7 UDP collector when
// ps5.ip is set, or a capture replay when replay-file is set; neither = analysis mode, no source, stored
// data only), the ingest gRPC server for external
// collectors (Session Management -> Processing), and the HTTP read API (Analysis -> Coaching ->
// Presentation). It serves until ctx is cancelled (SIGINT/SIGTERM) — including after a replay ends —
// then stops everything gracefully. Stores are in-memory or Postgres per config. reconfigure is an
// optional (nil-safe) channel of live heartbeat-interval updates for the collector.
func RunBackend(ctx context.Context, cfg config.BackendConfig, log *zap.Logger, reconfigure <-chan time.Duration) error {
	if log == nil { // defensive: match ingest.NewServer / NewLoggingSink nil-logger handling
		log = zap.NewNop()
	}

	// Select the storage backing (config-driven, D-B5 seam). One pool + one migration set (internal/db)
	// back both the session and lap stores. Postgres migrates on startup.
	var (
		sessStore  session.Store
		lapStore   processing.LapStore
		trackRefs  processing.TrackRefStore // the same lap store's per-track side (labels, pins)
		traceStore processing.TraceStore
	)
	switch cfg.Storage {
	case "postgres":
		pool, err := db.Connect(ctx, cfg.DBDSN)
		if err != nil {
			return fmt.Errorf("db connect: %w", err)
		}
		defer pool.Close()
		if err := db.Migrate(ctx, pool); err != nil {
			return fmt.Errorf("db migrate: %w", err)
		}
		sessStore = session.NewPostgresStore(pool)
		pgLaps := processing.NewPostgresLapStore(pool)
		lapStore, trackRefs = pgLaps, pgLaps
		traceStore = processing.NewPostgresTraceStore(pool)
		log.Info("using postgres stores")
	default:
		sessStore = session.NewMemStore()
		memLaps := processing.NewMemLapStore()
		memLaps.SessionTotalLaps = sessionTotalLaps(sessStore) // same session total the Postgres subquery reads
		lapStore, trackRefs = memLaps, memLaps
		traceStore = processing.NewMemTraceStore()
		log.Info("using in-memory stores")
	}

	// Real-time egress: the Hub taps every frame at ingest (publish-first); the async trace store keeps
	// storage writes off the ingest goroutine; the enrich sink refreshes live analysis per lap.
	hub := live.New()
	enrich := live.NewEnrichmentState()
	delta := live.NewDeltaTracker() // live per-frame gap-to-best-lap, fed on the ingest goroutine below

	traceStore = live.NewAsyncTraceStore(traceStore, 256, func(dropped int) {
		log.Warn("trace batch dropped (storage back-pressure)", zap.Int("dropped", dropped))
	})
	defer traceStore.(*live.AsyncTraceStore).Close()

	// Reference Knowledge: the read-only Track RKB DB. Default path $HOME/.garage/rkb.db; failing to
	// open it is fatal (the /v1/tracks endpoints and live track-detect depend on it) rather than
	// silently serving an empty RKB. Path is not a user config key -- a fixed convention. Opened here,
	// ahead of the session sink fan-out, because the live TrackDetector sink needs it as its
	// DetectionGeomProvider.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir for rkb: %w", err)
	}
	rkbPath := filepath.Join(home, ".garage", "rkb.db")
	rkbStore, err := rkb.Open(rkbPath)
	if err != nil {
		return fmt.Errorf("open rkb %s: %w", rkbPath, err)
	}
	defer func() { _ = rkbStore.Close() }()

	// slugFor maps a session id to its detected track slug from live enrichment (the TrackDetector
	// writes it there, and it is RETAINED after the session ends — see EnrichSink.OnSessionEnd), so the
	// RKB adapter can resolve real sectors for the live and just-finished session. Older sessions have
	// no slug here and fall back to mini-sectors (surfaced, not hidden, via the serving path's reason).
	slugFor := enrichSlugFor(enrich)
	// rkbAdapter bridges the turn-labeled RKB (centerline + sector labels) to reference.KB's arc-length
	// sector fractions. It backs BOTH the analyzer's KB (fixing live /v1/live sector geometry) and the
	// lap-serving path (processing.SectorProvider) via one instance, so a slug logs its missing-geometry
	// warning at most once across both consumers.
	rkbAdapter := rkbref.New(rkbStore, slugFor, log)

	// Fan out the session stream to the lap segmenter + trace recorder (Processing), a logging sink,
	// the enrichment sink, and the live track detector. Persistence failures are surfaced via error
	// hooks (the Sink/Observe API is error-less).
	segmenter := processing.NewLapSegmenter(lapStore)
	segmenter.OnError = func(err error) { log.Error("lap persistence failed", zap.Error(err)) }
	recorder := processing.NewTraceRecorder(traceStore)
	recorder.OnError = func(err error) { log.Error("trace persistence failed", zap.Error(err)) }
	// Capture the live delta-to-best onto each stored trace sample. Safe because ingest is publish-first
	// (ingest.go: Publisher.Publish -> Detector.Observe -> recorder.OnFrame), so delta.Delta() already
	// reflects the current frame when the recorder reads it. Inherits DeltaTracker's single-active-session
	// assumption (fine for the MVP one-collector case).
	recorder.DeltaFn = delta.Delta

	// Analysis -> Coaching (placeholder, D-B21). The KB is the live RKB adapter, so Analysis splits laps
	// on the RKB's real sector geometry (centerline + labels); it still falls back to mini-sectors when a
	// track is unresolved or lacks centerline/labels.
	analyzer := analysis.Analyzer{Laps: lapStore, Traces: traceStore, KB: rkbAdapter, FallbackSectors: 3}
	enrichSink := live.NewEnrichSink(analyzer, enrich, func(err error) { log.Error("enrichment failed", zap.Error(err)) })
	defer enrichSink.Close()

	// Live track detection: keeps matching the driven path's shape (and direction of travel) against the
	// RKB's reference geometry, publishing its best guess into enrich as "provisional" (it may switch, or
	// be withdrawn) until ~a full lap or a clear margin locks it "confident" for the session. Every
	// consumer reads the track from enrich, so a provisional switch propagates: slugFor (sector timing
	// + reference for laps analysed from then on), /v1/live and the SSE "track" event.
	//
	// Each lap stores the track the detector has when it completes (segmenter.TrackFn), and a lock
	// backfills its stint's unlabelled/provisional laps (OnLock: one UPDATE per lock, on the stream
	// goroutine like the lap saves). refLoader sits between the detector and enrich: on each lock it
	// loads the track's stored reference lap off the ingest goroutine for the live stored-ref delta.
	// segmenter.OnSaved feeds every stored lap to refLoader.LapStored, so the track's first stored lap
	// (or a new fastest one while unpinned) becomes the live reference without waiting for a re-lock.
	refLoader := live.NewRefLoader(enrich, delta, func(_ context.Context, slug string) (live.LoadedRef, error) {
		ref, ok, err := processing.ResolveTrackReference(trackRefs, slug, log)
		if err != nil || !ok {
			return live.LoadedRef{}, err
		}
		samples, err := traceStore.ByLap(ref.Lap.SessionID, ref.Lap.LapNumber)
		if err != nil {
			return live.LoadedRef{}, err
		}
		n := processing.Normalize(samples)
		log.Info("stored reference lap loaded for live delta", zap.String("slug", slug), zap.String("source", ref.Source),
			zap.String("session", ref.Lap.SessionID), zap.Int32("lap", ref.Lap.LapNumber), zap.Duration("lap_time", ref.Lap.Duration))
		return live.LoadedRef{Lap: &n, Pinned: ref.Source == processing.RefSourcePinned, LapTime: ref.Lap.Duration,
			SessionID: ref.Lap.SessionID, LapNumber: ref.Lap.LapNumber}, nil
	}, func(err error) { log.Error("stored reference load failed", zap.Error(err)) })
	defer refLoader.Close()
	detector := processing.NewTrackDetector(rkbStore, refLoader, log)
	detector.OnLock = func(ctx context.Context, sessionID string, span processing.TimeSpan, slug string) error {
		if err := trackRefs.LabelTrack(ctx, sessionID, span, slug); err != nil {
			log.Error("labelling laps with the locked track failed", zap.String("session", sessionID), zap.Error(err))
			return err
		}
		// The lock was published before this backfill, so the loader's first load may have missed the
		// laps just labelled: reload once now that they are eligible.
		refLoader.Reload(slug)
		return nil
	}
	// At session end (after the final lock backfill) a provisional label was never confirmed and was
	// often wrong (an unresolved track guessed as a neighbouring layout): clear it, once per session.
	detector.OnEnd = func(ctx context.Context, sessionID string, keep []processing.TimeSpan) {
		if err := trackRefs.ClearProvisionalLabels(ctx, sessionID, keep...); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				log.Warn("clearing unconfirmed track labels timed out; leaving them", zap.String("session", sessionID))
				return
			}
			log.Error("clearing unconfirmed track labels failed", zap.String("session", sessionID), zap.Error(err))
		}
	}
	// The user's "this is <track>" pick (POST /v1/live/track): committed to the detector's session at
	// once, where it wins over detection for that session. Its relabel (on the stream goroutine)
	// overwrites every lap of the session (the pick beats an earlier lock), then reloads the stored
	// reference once the laps are labelled, as OnLock does.
	override := processing.NewTrackOverride(detector)
	detector.OnUserTrack = func(ctx context.Context, sessionID, slug, confidence string) error {
		if err := trackRefs.SetSessionTrack(ctx, sessionID, slug, confidence); err != nil {
			log.Error("labelling laps with the user's track failed", zap.String("session", sessionID), zap.Error(err))
			return err
		}
		refLoader.Reload(slug)
		return nil
	}
	segmenter.TrackFn = detector.Track
	segmenter.OnSaved = func(l processing.Lap) {
		// The segmenter's lap does not carry the session total (the lap stores fill it at read time, from
		// the session row the Detector keeps current). Only lap 1 can be a race's excluded lap, so only
		// it costs a session lookup (one query per race in Postgres mode).
		if l.LapNumber == 1 {
			l.TotalLaps = sessionTotalLaps(sessStore)(l.SessionID)
		}
		refLoader.LapStored(l)
	}

	// Sink order matters for enrichSink and detector: detector.OnSessionEnd does its final match attempt
	// and SetTrack synchronously, inline, before returning; enrichSink.OnSessionEnd no longer clears or
	// replaces the published enrichment (it only fences its own map state), so whichever order these two
	// run in, neither can clobber the other's fields in the shared EnrichmentState. detector is still
	// registered before enrichSink for clarity/history, but the ordering is no longer load-bearing.
	// Logging/segmenter/recorder are independent persistence/logging sinks with no cross-sink field
	// interaction, so their relative order does not matter either.
	sink := session.NewMultiSink(ingest.NewLoggingSink(log), segmenter, recorder, detector, enrichSink)

	detCfg := session.Config{OnError: func(err error) { log.Error("session persistence failed", zap.Error(err)) }}
	withDedup(&detCfg, lapStore, traceStore, enrich, log)
	// A session's total_laps was just persisted: a lap 1 loaded as the live reference before it was known
	// is now a race's ineligible first lap, so the loader re-resolves (once per total change).
	detCfg.OnTotalLaps = func(rec *session.Record) { refLoader.TotalLapsKnown(rec.ID) }
	// A discarded session's laps (and any pin on them, by cascade) are gone: if the live stored
	// reference came from it, reload so the delta falls back to the track's fastest remaining lap.
	purge := detCfg.OnDiscard
	detCfg.OnDiscard = func(rec *session.Record, reason session.DiscardReason, orig string) {
		purge(rec, reason, orig)
		refLoader.SessionRemoved(rec.ID)
	}
	ingestSrv := ingest.NewServer(sessStore, sink, detCfg, log)
	ingestSrv.Publisher = live.NewMultiPublisher(hub, delta) // publish-first egress, fanned out to the Hub and the DeltaTracker

	gs := grpc.NewServer()
	telemetryv1.RegisterTelemetryServer(gs, ingestSrv)

	// Read side: Analysis -> Coaching -> HTTP API.
	apiSrv := api.NewServer(coach.New(analyzer), sessStore, hub, enrich, delta, rkbStore, log, lapStore, traceStore, rkbAdapter)
	apiSrv.TrackRefs = trackRefs
	apiSrv.OnReferenceChange = refLoader.Reload
	apiSrv.Mode = cfg.Mode()
	if cfg.Mode() == config.ModeAnalysis && cfg.Storage == "memory" {
		log.Warn("analysis mode with --storage=memory: nothing is stored, so there is nothing to analyse; use --storage=postgres")
	}
	apiSrv.AllowedHosts = api.MutationHosts(cfg.APIAddr, cfg.APIAllowedHosts)
	httpSrv := &http.Server{Addr: cfg.APIAddr, Handler: apiSrv.Handler(), ReadHeaderTimeout: 5 * time.Second}

	// Bind the ingest listener only once we're committed to serving (after storage init), so a failed
	// startup never leaks a listener.
	lis, err := net.Listen("tcp", cfg.IngestAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.IngestAddr, err)
	}

	// In-process telemetry: the collector (or a replay) publishes straight into local, which feeds the
	// same per-stream ingest loop as a gRPC stream. The gRPC listener above stays for external
	// collectors and the replay-client. External streams get their own Detector but share the live
	// Hub/DeltaTracker with this in-process source.
	local := ingest.NewLocal(ingestSrv, cfg.LiveBufferSize)
	// The user's Record switch drives ONLY this in-process stream's Detector (live collector or replay);
	// external gRPC collector streams always record.
	recording := session.NewRecordingControl()
	local.Recording = recording
	apiSrv.Recording = recording
	apiSrv.TrackPick = override
	source, err := newSource(cfg, log, local, reconfigure)
	if err != nil {
		_ = lis.Close()
		return err
	}

	// local runs on its own context, NOT ctx: it must outlive the source so it can drain the tail and
	// finalize the session only after the source has stopped publishing (see stopLocal).
	localCtx, cancelLocal := context.WithCancel(context.Background())
	localDone := make(chan struct{})
	go func() { local.Run(localCtx); close(localDone) }()
	var stopLocalOnce sync.Once
	stopLocal := func() { stopLocalOnce.Do(func() { cancelLocal(); <-localDone }) }

	// The servers and a live collector run on their own goroutines; the first error (or shutdown
	// signal) tears everything down.
	serveErr := make(chan error, 3)
	srcCtx, cancelSrc := context.WithCancel(ctx)
	srcDone := make(chan struct{})
	go func() {
		defer close(srcDone)
		if err := source(srcCtx, stopLocal); err != nil {
			serveErr <- err
		}
	}()
	go func() {
		log.Info("ingest gRPC server listening", zap.String("addr", cfg.IngestAddr))
		serveErr <- gs.Serve(lis)
	}()
	go func() {
		log.Info("http API server listening", zap.String("addr", cfg.APIAddr))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	// Shutdown order: stop the source (no new frames) -> stop local (drain + finalize the session) ->
	// stop the servers.
	shutdown := func() {
		cancelSrc()
		<-srcDone
		stopLocal()
		stopServers(gs, httpSrv)
	}
	select {
	case <-ctx.Done():
		log.Info("shutdown signal received; stopping servers")
		shutdown()
		return nil
	case err := <-serveErr:
		shutdown() // one component exited; bring the rest down gracefully too
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}
}

// newSource builds the in-process telemetry source selected by cfg and returns its run function. All
// startup failures (UDP bind, unreadable capture) surface here so rtps fails fast. The run function
// blocks until ctx is cancelled or the source ends; stopLocal ends the in-process stream (finalizing
// the session) and is called by a replay at EOF. A non-nil return is fatal to serving (only a live
// collector's receive failure); a replay's read error is logged and rtps keeps serving. With neither
// source configured (analysis mode) the source just waits for ctx; the ingest gRPC server keeps running
// so an external collector can still feed it.
func newSource(cfg config.BackendConfig, log *zap.Logger, local *ingest.Local, reconfigure <-chan time.Duration) (func(ctx context.Context, stopLocal func()) error, error) {
	switch {
	case cfg.PS5IP != "":
		col, err := newCollector(cfg, log, local)
		if err != nil {
			return nil, fmt.Errorf("collector: %w", err)
		}
		return func(ctx context.Context, _ func()) error {
			if err := col.run(ctx, reconfigure); err != nil {
				return fmt.Errorf("collector: %w", err)
			}
			return nil
		}, nil
	case cfg.ReplayFile != "":
		src, err := replay.Open(cfg.ReplayFile)
		if err != nil {
			return nil, fmt.Errorf("replay %s: %w", cfg.ReplayFile, err)
		}
		return func(ctx context.Context, stopLocal func()) error {
			defer func() { _ = src.Close() }()
			// Replay reuses the collector's exact decode path (no raw dump), so results match a live run.
			// Lossless, unlike the live path: replay has no kernel UDP buffer to protect, so it blocks on
			// a full buffer (fidelity > latency) rather than evicting frames and diverging from the
			// original run.
			pipeline := cingest.NewPipeline(nil, waitPublisher{ctx: ctx, l: local})
			log.Info("replay starting", zap.String("file", cfg.ReplayFile), zap.Float64("speed", cfg.ReplaySpeed))
			err := src.Play(ctx, uint16(cfg.RecvPort), cfg.ReplaySpeed, pipeline.Handle)
			switch {
			case err == nil:
				stopLocal() // EOF: end the stream so the detector finalizes the session; keep serving
				logCounters(log, "replay finished", pipeline, local)
			case ctx.Err() != nil:
				logCounters(log, "replay stopped", pipeline, local)
			default:
				stopLocal()
				log.Error("replay failed; still serving", zap.String("file", cfg.ReplayFile), zap.Error(err))
				logCounters(log, "replay stopped", pipeline, local)
			}
			return nil
		}, nil
	default:
		return func(ctx context.Context, _ func()) error { <-ctx.Done(); return nil }, nil
	}
}

// waitPublisher adapts Local.PublishWait to collector/ingest.Publisher for the replay source. A
// publish error means ctx was cancelled or the local stream stopped; either way replay is ending
// (Play observes ctx itself), so the frame is simply not delivered.
type waitPublisher struct {
	ctx context.Context
	l   *ingest.Local
}

// Publish blocks until the frame is queued on the local ingest stream (lossless replay), dropping it
// only when replay is already ending (see the type comment).
func (p waitPublisher) Publish(f *telemetryv1.TelemetryFrame) { _ = p.l.PublishWait(p.ctx, f) }

// stopServers shuts the gRPC and HTTP servers down CONCURRENTLY, each bounded by a timeout so a
// long-lived ingest stream (which GracefulStop would otherwise wait on) can't stall the HTTP
// shutdown. gRPC falls back to a hard Stop, HTTP to its Shutdown deadline.
func stopServers(gs *grpc.Server, httpSrv *http.Server) {
	const grace = 5 * time.Second
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		done := make(chan struct{})
		go func() { gs.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-time.After(grace):
			gs.Stop() // force-close remaining streams
		}
	}()
	go func() {
		defer wg.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()
	wg.Wait()
}

// withDedup wires session discarding into cfg. A confirmed session is discarded (rolled back) when its
// completed lap's fingerprint is already stored under another session (an in-game replay of stored laps,
// or the same capture ingested twice), or when it ends without a completed timed lap. The Detector
// deletes the session row and stops forwarding frames; this purges the laps and trace samples its sinks
// already wrote (traces must be the AsyncTraceStore wrapper so queued batches are discarded too) and
// logs once. A duplicate points the live view at the original session; any other discard clears the
// live session link (session_id ""). The same rollback serves
// the user's Record switch (DiscardRecordingOff).
func withDedup(cfg *session.Config, laps processing.LapStore, traces processing.TraceStore, enrich *live.EnrichmentState, log *zap.Logger) {
	cfg.FingerprintOwner = func(fp string) (string, bool) {
		id, found, err := laps.SessionByFingerprint(fp)
		if err != nil {
			// Skip the dedup decision rather than guess: the session is kept. If it really was a
			// duplicate, its lap still stores (the segmenter drops the taken fingerprint), so the
			// session stays consistent.
			log.Warn("fingerprint lookup failed; dedup skipped", zap.Error(err))
			return "", false
		}
		return id, found
	}
	cfg.OnLateDuplicate = func(rec *session.Record, other string) {
		log.Warn("later lap matches another stored session; session kept",
			zap.String("session", rec.ID), zap.String("other", other))
	}
	cfg.OnDiscard = func(rec *session.Record, reason session.DiscardReason, orig string) {
		if err := laps.DeleteSession(rec.ID); err != nil {
			log.Error("purge discarded laps failed", zap.String("session", rec.ID), zap.Error(err))
		}
		if err := traces.DeleteSession(rec.ID); err != nil {
			log.Error("purge discarded trace failed", zap.String("session", rec.ID), zap.Error(err))
		}
		switch reason {
		case session.DiscardDuplicate:
			enrich.SetDuplicate(orig)
			log.Info("duplicate session dropped", zap.String("session", rec.ID), zap.String("duplicate_of", orig),
				zap.Int32("car_id", rec.CarID))
		case session.DiscardNoCompletedLap:
			enrich.ClearSession() // the session no longer exists: no live link to it
			log.Info("session dropped: no completed lap", zap.String("session", rec.ID), zap.Int32("car_id", rec.CarID))
		case session.DiscardRecordingOff:
			enrich.ClearSession()
			// Logged by the API handler that switched recording off (once per toggle).
		}
	}
	cfg.OnDuplicateDiverged = func(rec *session.Record, orig string) {
		log.Warn("dropped duplicate session has a lap not in the original; still dropping",
			zap.String("session", rec.ID), zap.String("duplicate_of", orig))
	}
}

// sessionTotalLaps returns a lookup of a session's stored total_laps (0 when absent).
func sessionTotalLaps(st session.Store) func(string) int32 {
	return func(id string) int32 {
		if r, ok := st.Get(id); ok {
			return r.TotalLaps
		}
		return 0
	}
}

// enrichSlugFor returns the session-to-track lookup the RKB adapter uses (rkbref.New): the track
// published in live enrichment for sessionID, if any. A track the detector only guessed is withdrawn
// from enrichment when the session ends (TrackDetector.OnSessionEnd), so it never reaches this lookup
// for a finished session.
func enrichSlugFor(enrich *live.EnrichmentState) func(sessionID string) (string, bool) {
	return func(sessionID string) (string, bool) {
		en := enrich.Get()
		if en.SessionID == sessionID && en.Track != nil {
			return *en.Track, true
		}
		return "", false
	}
}
