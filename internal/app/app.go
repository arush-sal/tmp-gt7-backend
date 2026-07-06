package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/arush-sal/garage/backend/collector/ingest"
	"github.com/arush-sal/garage/backend/collector/rawsink"
	"github.com/arush-sal/garage/backend/collector/transport"
	"github.com/arush-sal/garage/backend/internal/config"
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
//
// reconfigure is an optional (nil-safe) channel of live heartbeat-interval updates: the config
// hot-reload watcher in main sends on it when heartbeat.interval changes, and Run forwards each
// value into the Receiver's SetHeartbeatInterval without needing a restart. A nil channel disables
// this (callers who don't want hot-reload just pass nil; a nil channel blocks forever in a select,
// which is exactly "never fires").
func Run(ctx context.Context, cfg config.Config, log *zap.Logger, reconfigure <-chan time.Duration) error {
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
	g.Go(func() error { report(gctx, log, pipeline, pub, raw, cfg.RecvPort); return nil })
	g.Go(func() error {
		// The receiver is the driver; when it stops, tear everything down.
		defer cancel()
		return recv.Run(gctx)
	})
	g.Go(func() error { applyHeartbeatReconfigures(gctx, recv, reconfigure); return nil })

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
		zap.Uint64("valid", c.Valid),
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

// applyHeartbeatReconfigures forwards live heartbeat-interval updates from the config hot-reload
// watcher into the Receiver until ctx is cancelled. A nil reconfigure channel makes the receive
// case permanently block, so this goroutine just parks until shutdown — the nil-channel-disables
// pattern the doc comment on Run promises.
func applyHeartbeatReconfigures(ctx context.Context, recv *ingest.Receiver, reconfigure <-chan time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-reconfigure:
			if !ok {
				return
			}
			recv.SetHeartbeatInterval(d)
		}
	}
}

// report periodically logs the pipeline counters and drop counts at INFO. It runs on its own
// goroutine so the receive hot path only ever touches atomic counters, never the logger.
func report(ctx context.Context, log *zap.Logger, p *ingest.Pipeline, pub *transport.Publisher, raw rawsink.RawSink, recvPort int) {
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
				zap.Uint64("valid", c.Valid),
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
			// received = every inbound datagram (valid GT7 + foreign + wrong-length). Zero means the
			// PS5 isn't reaching us at all — surface it loudly rather than a quiet all-zeros stat.
			if c.Valid+c.Foreign+c.Short == 0 {
				log.Warn("no UDP telemetry received — is GT7 running on the console, and is inbound UDP allowed on the recv port?",
					append(fields, zap.Int("recv_port", recvPort))...)
			} else {
				log.Info("telemetry stats", fields...)
			}
		}
	}
}
