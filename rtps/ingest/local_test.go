package ingest

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
)

// TestLocal_DropOldestWhenFull: with nobody draining, a full buffer evicts the OLDEST frame so the
// freshest telemetry survives, and every eviction is counted. Publish must never block.
func TestLocal_DropOldestWhenFull(t *testing.T) {
	srv := NewServer(session.NewMemStore(), session.NoopSink{}, session.Config{}, zap.NewNop())
	l := NewLocal(srv, 2)
	for i := range 5 {
		l.Publish(mkFrame(i*100, true, 1384, uint32(i+1), 2, 2)) // no Run: nothing drains
	}
	if got := l.Dropped(); got != 3 {
		t.Fatalf("Dropped = %d, want 3", got)
	}
	// The two survivors are the newest two (pkg 4, 5), in order.
	for _, want := range []uint32{4, 5} {
		if f := <-l.ch; f.GetPackageId() != want {
			t.Errorf("buffered PackageId = %d, want %d", f.GetPackageId(), want)
		}
	}
}

// TestLocal_RunDeliversInOrderAndFinalizes: frames published before Run reach the Server's sink path
// in order, and cancelling Run is the end-of-stream signal that closes the Detector (the in-progress
// session is finalized into the store, exactly like a gRPC stream EOF).
func TestLocal_RunDeliversInOrderAndFinalizes(t *testing.T) {
	store := session.NewMemStore()
	srv := NewServer(store, session.NoopSink{}, session.Config{}, zap.NewNop())
	pub := &recordingPub{}
	srv.Publisher = pub

	// A confirmed in-race stint with NO trailing menu frames: only end-of-stream can close it.
	var frames []*telemetryv1.TelemetryFrame
	for i := range 12 {
		frames = append(frames, mkFrame(i*100, true, 1384, uint32(i+1), 2, 2))
		frames[i].Sequence = uint64(i + 1)
	}
	l := NewLocal(srv, len(frames))
	for _, f := range frames {
		l.Publish(f)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: Run must still drain the buffered tail before ending the stream
	done := make(chan uint64, 1)
	go func() { done <- l.Run(ctx) }()

	select {
	case n := <-done:
		if n != uint64(len(frames)) {
			t.Errorf("Run frames = %d, want %d", n, len(frames))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	for i, s := range pub.seqs {
		if s != uint64(i+1) {
			t.Fatalf("frame %d had sequence %d, want %d (out of order)", i, s, i+1)
		}
	}
	if len(pub.seqs) != len(frames) {
		t.Fatalf("published %d frames, want %d", len(pub.seqs), len(frames))
	}
	if n := len(store.List()); n != 1 {
		t.Errorf("store has %d sessions, want 1 (end-of-stream must close the detector)", n)
	}
}

// waitPublish runs PublishWait in a goroutine and returns its result channel.
func waitPublish(l *Local, ctx context.Context, pkg uint32) <-chan error {
	c := make(chan error, 1)
	go func() { c <- l.PublishWait(ctx, mkFrame(0, true, 1384, pkg, 1, 2)) }()
	return c
}

// TestLocal_PublishWaitBlocksNotEvicts: with a full buffer and a stalled consumer PublishWait blocks
// (no eviction, Dropped stays 0) and completes once a slot frees, preserving order.
func TestLocal_PublishWaitBlocksNotEvicts(t *testing.T) {
	srv := NewServer(session.NewMemStore(), session.NoopSink{}, session.Config{}, zap.NewNop())
	l := NewLocal(srv, 1)
	l.Publish(mkFrame(0, true, 1384, 1, 1, 2))
	res := waitPublish(l, context.Background(), 2)
	select {
	case err := <-res:
		t.Fatalf("PublishWait returned %v on a full buffer, want it to block", err)
	case <-time.After(50 * time.Millisecond):
	}
	if l.Dropped() != 0 {
		t.Fatalf("Dropped = %d, want 0", l.Dropped())
	}
	if f := <-l.ch; f.GetPackageId() != 1 {
		t.Fatalf("first frame pkg = %d, want 1", f.GetPackageId())
	}
	if err := <-res; err != nil {
		t.Fatalf("PublishWait after slot freed: %v", err)
	}
	if f := <-l.ch; f.GetPackageId() != 2 {
		t.Fatalf("second frame pkg = %d, want 2", f.GetPackageId())
	}
}

// TestLocal_PublishWaitCtxCancelUnblocks: cancelling ctx releases a blocked PublishWait.
func TestLocal_PublishWaitCtxCancelUnblocks(t *testing.T) {
	srv := NewServer(session.NewMemStore(), session.NoopSink{}, session.Config{}, zap.NewNop())
	l := NewLocal(srv, 1)
	l.Publish(mkFrame(0, true, 1384, 1, 1, 2))
	ctx, cancel := context.WithCancel(context.Background())
	res := waitPublish(l, ctx, 2)
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-res:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PublishWait did not return after ctx cancel")
	}
}

// TestLocal_PublishWaitStopUnblocks: once Run has returned (Local stopped), a blocked or new
// PublishWait returns io.ErrClosedPipe instead of waiting on a consumer that is gone.
func TestLocal_PublishWaitStopUnblocks(t *testing.T) {
	srv := NewServer(session.NewMemStore(), session.NoopSink{}, session.Config{}, zap.NewNop())
	l := NewLocal(srv, 1)
	runCtx, stop := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() { l.Run(runCtx); close(runDone) }()
	// Fill the buffer faster than... simply stop Run first, then fill so nothing drains.
	stop()
	<-runDone
	l.Publish(mkFrame(0, true, 1384, 1, 1, 2)) // buffer full, consumer gone
	select {
	case err := <-waitPublish(l, context.Background(), 2):
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("err = %v, want io.ErrClosedPipe", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PublishWait did not return after Run stopped")
	}
}
