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

	time.Sleep(200 * time.Millisecond)
	cancel()

	// The 250ms backoff bounds how many connection attempts can occur within 200ms: at most a
	// couple (initial dial + maybe one retry right at the boundary). A busy-spin bug would produce
	// hundreds/thousands of attempts in the same window.
	got := attempts.Load()
	if got > 3 {
		t.Fatalf("observed %d connection attempts in 200ms; expected busy-spin to be bounded by backoff", got)
	}
	if got < 1 {
		t.Fatalf("observed %d connection attempts; expected at least 1", got)
	}
}
