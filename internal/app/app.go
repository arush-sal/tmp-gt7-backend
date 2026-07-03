package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/arush-sal/gt7/backend/collector/ingest"
	"github.com/arush-sal/gt7/backend/collector/rawsink"
	"github.com/arush-sal/gt7/backend/collector/transport"
	"github.com/arush-sal/gt7/backend/internal/config"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// reportInterval is how often the collector logs its telemetry counters (off the hot path).
const reportInterval = 10 * time.Second

// Run wires the collector together and drives it until ctx is cancelled. Composition order is
// bottom-up (publisher -> pipeline -> receiver); shutdown order is the reverse and enforced here:
// the receiver stops first (no new packets), then the publisher winds down, and finally the raw
// dump is flushed and closed.
func Run(ctx context.Context, cfg config.Config, log *zap.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Optional debug raw dump.
	var raw rawsink.RawSink
	if cfg.RawDumpEnabled {
		path := filepath.Join(cfg.RawDumpDir, fmt.Sprintf("raw-%d.bin", time.Now().Unix()))
		ls, err := rawsink.NewLocalSink(afero.NewOsFs(), path, cfg.RawBufferSize)
		if err != nil {
			return fmt.Errorf("app: raw sink: %w", err)
		}
		ls.SetLogger(log)
		raw = ls
		log.Info("raw dump enabled", zap.String("path", path))
	}

	pub := transport.NewPublisher(cfg.RTPSAddr, cfg.LiveBufferSize,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	pub.SetLogger(log)

	pipeline := ingest.NewPipeline(raw, pub)

	recv, err := ingest.NewReceiver(cfg.RecvPort, cfg.SendPort, cfg.PS5IP, cfg.HeartbeatInterval, pipeline.Handle)
	if err != nil {
		return fmt.Errorf("app: receiver: %w", err)
	}
	recv.SetLogger(log)

	log.Info("collector starting",
		zap.String("ps5", cfg.PS5IP),
		zap.Stringer("recv_addr", recv.LocalAddr()),
		zap.Int("send_port", cfg.SendPort),
		zap.String("rtps", cfg.RTPSAddr),
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return pub.Run(gctx) })
	g.Go(func() error { report(gctx, log, pipeline, pub, raw); return nil })
	g.Go(func() error {
		// The receiver is the driver; when it stops, tear everything down.
		defer cancel()
		return recv.Run(gctx)
	})

	runErr := g.Wait()

	// Receiver (and everything else) has stopped: safe to flush the raw dump now.
	if raw != nil {
		if err := raw.Close(); err != nil {
			log.Warn("raw sink close", zap.Error(err))
		}
	}

	c := pipeline.Counters()
	log.Info("collector stopped",
		zap.Uint64("published", c.Published),
		zap.Uint64("foreign", c.Foreign),
		zap.Uint64("short", c.Short),
		zap.Uint64("stale", c.Stale),
		zap.Uint64("gaps", c.Gaps),
		zap.Uint64("live_dropped", pub.Dropped()),
	)

	// A cancelled context is a clean shutdown, not an error.
	if errors.Is(runErr, context.Canceled) {
		return nil
	}
	return runErr
}

// report periodically logs the pipeline counters and drop counts at INFO. It runs on its own
// goroutine so the receive hot path only ever touches atomic counters, never the logger.
func report(ctx context.Context, log *zap.Logger, p *ingest.Pipeline, pub *transport.Publisher, raw rawsink.RawSink) {
	t := time.NewTicker(reportInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c := p.Counters()
			fields := []zap.Field{
				zap.Uint64("published", c.Published),
				zap.Uint64("foreign", c.Foreign),
				zap.Uint64("short", c.Short),
				zap.Uint64("stale", c.Stale),
				zap.Uint64("resets", c.Resets),
				zap.Uint64("gaps", c.Gaps),
				zap.Uint64("live_dropped", pub.Dropped()),
			}
			if ls, ok := raw.(*rawsink.LocalSink); ok {
				fields = append(fields, zap.Uint64("raw_dropped", ls.Dropped()))
			}
			log.Info("telemetry stats", fields...)
		}
	}
}
