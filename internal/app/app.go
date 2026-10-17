package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	cingest "github.com/arush-sal/garage/backend/collector/ingest"
	"github.com/arush-sal/garage/backend/collector/rawsink"
	"github.com/arush-sal/garage/backend/internal/config"
	"github.com/arush-sal/garage/backend/rtps/ingest"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// reportInterval is how often the collector logs its telemetry counters (off the hot path).
const reportInterval = 10 * time.Second

// collector is the in-process GT7 UDP collector: receiver (socket + heartbeat) -> pipeline
// (decrypt/dedup/decode/map) -> pub (the rtps in-process ingest), with an optional debug raw dump.
// newCollector does everything that can fail at startup (raw dump file, UDP bind); run drives it.
type collector struct {
	raw      rawsink.RawSink // nil when the raw dump is disabled
	pipeline *cingest.Pipeline
	recv     *cingest.Receiver
	pub      *ingest.Local
	recvPort int
	log      *zap.Logger
}

// newCollector opens the optional raw dump and binds the UDP receive socket, publishing decoded frames
// into pub. A bind failure (e.g. the recv port already in use) is returned so rtps fails startup.
func newCollector(cfg config.BackendConfig, log *zap.Logger, pub *ingest.Local) (*collector, error) {
	var raw rawsink.RawSink
	if cfg.RawDumpEnabled {
		path := filepath.Join(cfg.RawDumpDir, fmt.Sprintf("raw-%d.bin", time.Now().Unix()))
		ls, err := rawsink.NewLocalSink(afero.NewOsFs(), path, cfg.RawBufferSize)
		if err != nil {
			return nil, fmt.Errorf("raw sink: %w", err)
		}
		ls.SetLogger(log)
		raw = ls
		log.Info("raw dump enabled", zap.String("path", path))
	}
	pipeline := cingest.NewPipeline(raw, pub)
	recv, err := cingest.NewReceiver(cfg.RecvPort, cfg.SendPort, cfg.PS5IP, cfg.HeartbeatInterval, pipeline.Handle)
	if err != nil {
		if raw != nil {
			_ = raw.Close()
		}
		return nil, err
	}
	recv.SetLogger(log)
	log.Info("collector starting",
		zap.String("ps5", cfg.PS5IP),
		zap.Stringer("recv_addr", recv.LocalAddr()),
		zap.Int("send_port", cfg.SendPort))
	return &collector{raw: raw, pipeline: pipeline, recv: recv, pub: pub, recvPort: cfg.RecvPort, log: log}, nil
}

// run drives the collector until ctx is cancelled (nil) or the receiver fails (its error). Shutdown
// order is enforced here: the receiver stops first (heartbeat stops, the socket closes, so no new
// packets), the stats reporter and reconfigure forwarder wind down, and only then is the raw dump
// flushed and closed. Closing the publisher (ending the in-process stream, finalizing the session) is
// the caller's last step, after run returns, so no frame is published into a closed stream.
//
// reconfigure is an optional (nil-safe) channel of live heartbeat-interval updates from the config
// hot-reload watcher; a nil channel never fires.
func (c *collector) run(ctx context.Context, reconfigure <-chan time.Duration) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { report(gctx, c.log, c.pipeline, c.pub, c.raw, c.recvPort); return nil })
	g.Go(func() error {
		// The receiver is the driver; when it stops, tear everything down.
		defer cancel()
		return c.recv.Run(gctx)
	})
	g.Go(func() error { applyHeartbeatReconfigures(gctx, c.recv, reconfigure); return nil })
	runErr := g.Wait()

	// Receiver (and everything else) has stopped: safe to flush the raw dump now.
	if c.raw != nil {
		if err := c.raw.Close(); err != nil {
			c.log.Warn("raw sink close", zap.Error(err))
		}
	}
	logCounters(c.log, "collector stopped", c.pipeline, c.pub)

	// A cancelled context is a clean shutdown, not an error.
	if errors.Is(runErr, context.Canceled) {
		return nil
	}
	return runErr
}

// logCounters logs a final snapshot of the pipeline counters and in-process drops.
func logCounters(log *zap.Logger, msg string, p *cingest.Pipeline, pub *ingest.Local) {
	c := p.Counters()
	log.Info(msg,
		zap.Uint64("published", c.Published),
		zap.Uint64("valid", c.Valid),
		zap.Uint64("foreign", c.Foreign),
		zap.Uint64("short", c.Short),
		zap.Uint64("stale", c.Stale),
		zap.Uint64("resets", c.Resets),
		zap.Uint64("gaps", c.Gaps),
		zap.Uint64("live_dropped", pub.Dropped()),
	)
}

// applyHeartbeatReconfigures forwards live heartbeat-interval updates from the config hot-reload
// watcher into the Receiver until ctx is cancelled. A nil reconfigure channel makes the receive
// case permanently block, so this goroutine just parks until shutdown — the nil-channel-disables
// pattern the doc comment on collector.run promises.
func applyHeartbeatReconfigures(ctx context.Context, recv *cingest.Receiver, reconfigure <-chan time.Duration) {
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
func report(ctx context.Context, log *zap.Logger, p *cingest.Pipeline, pub *ingest.Local, raw rawsink.RawSink, recvPort int) {
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
