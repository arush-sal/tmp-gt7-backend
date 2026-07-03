package app

import (
	"context"
	"fmt"
	"net"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/internal/config"
	"github.com/arush-sal/garage/backend/rtps/ingest"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// RunBackend starts the backend (RTPS) process: it binds the ingest gRPC server and serves until ctx
// is cancelled (SIGINT/SIGTERM), then stops gracefully. Slice 1 wires an in-memory session store and
// a logging sink; persistence and downstream domains slot in here later.
func RunBackend(ctx context.Context, cfg config.BackendConfig, log *zap.Logger) error {
	if log == nil { // defensive: match ingest.NewServer / NewLoggingSink nil-logger handling
		log = zap.NewNop()
	}
	lis, err := net.Listen("tcp", cfg.IngestAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.IngestAddr, err)
	}

	// Select the session store backing (config-driven, D-B5 seam). Postgres migrates on startup.
	var store session.Store
	switch cfg.Storage {
	case "postgres":
		ps, err := session.NewPostgresStore(ctx, cfg.DBDSN)
		if err != nil {
			return fmt.Errorf("postgres store: %w", err)
		}
		defer ps.Close()
		if err := ps.Migrate(ctx); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		log.Info("using postgres session store")
		store = ps
	default:
		log.Info("using in-memory session store")
		store = session.NewMemStore()
	}

	sink := ingest.NewLoggingSink(log)
	srv := ingest.NewServer(store, sink, session.Config{}, log)

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
