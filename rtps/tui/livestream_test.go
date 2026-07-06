package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamLive_EmitsSnapshots(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = fmt.Fprintf(w, ": ping\n\n") // comment must be skipped
		_, _ = fmt.Fprintf(w, "data: {\"status\":\"live\",\"frame\":{\"car_speed\":100,\"current_gear\":3}}\n\n")
		fl.Flush()
		_, _ = fmt.Fprintf(w, "data: {\"status\":\"live\",\"frame\":{\"car_speed\":150,\"current_gear\":4}}\n\n")
		fl.Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snaps, _ := NewClient(ts.URL).StreamLive(ctx)

	got := func() LiveSnapshot {
		select {
		case s := <-snaps:
			return s
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for snapshot")
			return LiveSnapshot{}
		}
	}
	s1 := got()
	if s1.Frame == nil || s1.Frame.CarSpeed != 100 {
		t.Errorf("snapshot 1 = %+v", s1.Frame)
	}
	s2 := got()
	if s2.Frame == nil || s2.Frame.CarSpeed != 150 {
		t.Errorf("snapshot 2 = %+v", s2.Frame)
	}
}

// TestStreamLive_NonOKStatus verifies that a non-2xx response is surfaced as an error rather than
// being scanned as an SSE body, and that no snapshot is ever produced from it.
func TestStreamLive_NonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	snaps, errs := NewClient(ts.URL).StreamLive(ctx)

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected non-nil error for 404 response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error")
	}

	select {
	case s, ok := <-snaps:
		if ok {
			t.Fatalf("unexpected snapshot from 404 response: %+v", s)
		}
	case <-time.After(50 * time.Millisecond):
		// no snapshot arrived, as expected
	}
}

// TestStreamLive_BackoffOnCleanEOF verifies that a server which accepts and immediately closes the
// stream (a clean EOF, readStream returns nil) does not trigger a busy-spin reconnect loop: the
// client must still pause for the backoff interval before redialing.
func TestStreamLive_BackoffOnCleanEOF(t *testing.T) {
	var attempts atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Return immediately: clean EOF, no data, no error.
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, errs := NewClient(ts.URL).StreamLive(ctx)
	// Drain errs so a full buffer never blocks the reconnect loop.
	go func() {
		for range errs {
		}
	}()

	// Step 1: wait (bounded) for the FIRST connection attempt. Polling instead of a fixed sleep
	// means we start measuring only once the stream goroutine has actually dialed — on a slow
	// runner the first dial may not have happened within an arbitrary fixed window.
	deadline := time.Now().Add(2 * time.Second)
	for attempts.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("no connection attempt observed within 2s; stream never started")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Step 2: from a known baseline, measure how many further attempts land within one backoff
	// window (~250ms). With the 250ms backoff in effect, reconnects are spaced ~250ms apart, so at
	// most one or two new attempts can occur in this window. A busy-spin bug (no backoff) would
	// produce hundreds/thousands. The window sleep here measures a rate; it is not a sleep-as-
	// assertion for whether the stream started (that is Step 1's job).
	baseline := attempts.Load()
	time.Sleep(250 * time.Millisecond)
	delta := attempts.Load() - baseline
	cancel()

	if delta > 2 {
		t.Fatalf("observed %d new connection attempts within a 250ms backoff window; expected backoff to bound reconnects", delta)
	}
}
