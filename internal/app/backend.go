package app

import (
	"context"
	"fmt"
	"net"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/internal/config"
	"github.com/arush-sal/garage/backend/internal/db"
	"github.com/arush-sal/garage/backend/rtps/ingest"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// RunBackend starts the backend (RTPS) process: it binds the ingest gRPC server and serves until ctx
// is cancelled (SIGINT/SIGTERM), then stops gracefully. It wires the session store, the lap
// segmenter, and a logging sink (fanned out) — in-memory or Postgres per config.
func RunBackend(ctx context.Context, cfg config.BackendConfig, log *zap.Logger) error {
	if log == nil { // defensive: match ingest.NewServer / NewLoggingSink nil-logger handling
		log = zap.NewNop()
	}
	lis, err := net.Listen("tcp", cfg.IngestAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.IngestAddr, err)
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

	// Fan out the session stream to the lap segmenter + trace recorder (Processing) and a logging
	// sink. Persistence failures are surfaced via error hooks (the Sink/Observe API is error-less).
	segmenter := processing.NewLapSegmenter(lapStore)
	segmenter.OnError = func(err error) { log.Error("lap persistence failed", zap.Error(err)) }
	recorder := processing.NewTraceRecorder(traceStore)
	recorder.OnError = func(err error) { log.Error("trace persistence failed", zap.Error(err)) }
	sink := session.NewMultiSink(ingest.NewLoggingSink(log), segmenter, recorder)

	detCfg := session.Config{OnError: func(err error) { log.Error("session persistence failed", zap.Error(err)) }}
	srv := ingest.NewServer(sessStore, sink, detCfg, log)

	gs := grpc.NewServer()
	telemetryv1.RegisterTelemetryServer(gs, srv)

	// Serve on its own goroutine so we can wait on either a serve error or the shutdown signal.
	serveErr := make(chan error, 1)
	go func() {
		log.Info("rtps ingest server listening", zap.String("addr", cfg.IngestAddr))
		serveErr <- gs.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received; stopping ingest server")
		gs.GracefulStop() // drains in-flight streams, then Serve returns nil
		return nil
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("grpc serve: %w", err)
		}
		return nil
	}
}
