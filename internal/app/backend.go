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

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/internal/config"
	"github.com/arush-sal/garage/backend/internal/db"
	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/api"
	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/ingest"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// RunBackend starts the backend (RTPS) process: the ingest gRPC server (Session Management ->
// Processing) and the HTTP read API (Analysis -> Coaching -> Presentation). It serves until ctx is
// cancelled (SIGINT/SIGTERM), then stops both gracefully. Stores are in-memory or Postgres per config.
func RunBackend(ctx context.Context, cfg config.BackendConfig, log *zap.Logger) error {
	if log == nil { // defensive: match ingest.NewServer / NewLoggingSink nil-logger handling
		log = zap.NewNop()
	}

	// Select the storage backing (config-driven, D-B5 seam). One pool + one migration set (internal/db)
	// back both the session and lap stores. Postgres migrates on startup.
	var (
		sessStore  session.Store
		lapStore   processing.LapStore
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
		lapStore = processing.NewPostgresLapStore(pool)
		traceStore = processing.NewPostgresTraceStore(pool)
		log.Info("using postgres stores")
	default:
		sessStore = session.NewMemStore()
		lapStore = processing.NewMemLapStore()
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

	// Fan out the session stream to the lap segmenter + trace recorder (Processing), a logging sink,
	// and the enrichment sink. Persistence failures are surfaced via error hooks (the Sink/Observe API
	// is error-less).
	segmenter := processing.NewLapSegmenter(lapStore)
	segmenter.OnError = func(err error) { log.Error("lap persistence failed", zap.Error(err)) }
	recorder := processing.NewTraceRecorder(traceStore)
	recorder.OnError = func(err error) { log.Error("trace persistence failed", zap.Error(err)) }

	// Analysis -> Coaching (placeholder, D-B21). The KB is a stub for now, so Analysis falls back to
	// mini-sectors (the pipeline works without the real RKB).
	analyzer := analysis.Analyzer{Laps: lapStore, Traces: traceStore, KB: reference.NewStaticKB(), FallbackSectors: 3}
	enrichSink := live.NewEnrichSink(analyzer, enrich, func(err error) { log.Error("enrichment failed", zap.Error(err)) })
	defer enrichSink.Close()

	sink := session.NewMultiSink(ingest.NewLoggingSink(log), segmenter, recorder, enrichSink)

	detCfg := session.Config{OnError: func(err error) { log.Error("session persistence failed", zap.Error(err)) }}
	ingestSrv := ingest.NewServer(sessStore, sink, detCfg, log)
	ingestSrv.Publisher = live.NewMultiPublisher(hub, delta) // publish-first egress, fanned out to the Hub and the DeltaTracker

	gs := grpc.NewServer()
	telemetryv1.RegisterTelemetryServer(gs, ingestSrv)

	// Reference Knowledge: the read-only Track RKB DB. Default path $HOME/.garage/rkb.db; failing to
	// open it is fatal (the /v1/tracks endpoints and live track-detect depend on it) rather than
	// silently serving an empty RKB. Path is not a user config key -- a fixed convention.
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

	// Read side: Analysis -> Coaching -> HTTP API.
	apiSrv := api.NewServer(coach.New(analyzer), sessStore, hub, enrich, delta, rkbStore, log)
	httpSrv := &http.Server{Addr: cfg.APIAddr, Handler: apiSrv.Handler(), ReadHeaderTimeout: 5 * time.Second}

	// Bind the ingest listener only once we're committed to serving (after storage init), so a failed
	// startup never leaks a listener.
	lis, err := net.Listen("tcp", cfg.IngestAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.IngestAddr, err)
	}

	// Both servers run on their own goroutine; the first error (or shutdown signal) tears down both.
	serveErr := make(chan error, 2)
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

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received; stopping servers")
		stopServers(gs, httpSrv)
		return nil
	case err := <-serveErr:
		stopServers(gs, httpSrv) // one server exited; bring the other down gracefully too
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}
}

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
