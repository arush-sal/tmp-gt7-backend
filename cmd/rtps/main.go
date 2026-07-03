// Command rtps is the GARAGE backend: it hosts the telemetry ingest gRPC server that the collector
// streams into, and (in later slices) the Processing/Analysis/Coaching/API domains. Slice 1 = ingest
// + session-boundary detection with in-memory storage.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/arush-sal/garage/backend/internal/app"
	"github.com/arush-sal/garage/backend/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	cfg, err := config.LoadBackend(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}

	log, err := newLogger(cfg.LogLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", err)
		os.Exit(2)
	}
	defer func() { _ = log.Sync() }()

	// Ctrl-C / SIGTERM cancels the context, triggering a graceful gRPC shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.RunBackend(ctx, cfg, log); err != nil {
		log.Error("rtps backend failed", zap.Error(err))
		_ = log.Sync()
		os.Exit(1)
	}
}

// newLogger builds a structured (JSON) zap logger at the given level (debug|info|warn|error).
func newLogger(level string) (*zap.Logger, error) {
	var l zapcore.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}
	c := zap.NewProductionConfig()
	c.Level = zap.NewAtomicLevelAt(l)
	return c.Build()
}
