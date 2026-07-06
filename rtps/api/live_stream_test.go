package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/live"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLiveStream_EmitsOnPublish(t *testing.T) {
	hub := live.New()
	// coach.Coach is a struct (not an interface) so the brief's nil literal doesn't compile here;
	// the zero value is fine since this test never exercises the coaching handler.
	srv := NewServer(coach.Coach{}, nil, hub, live.NewEnrichmentState(), nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/live/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// give the subscriber goroutine a moment to register, then publish.
	go func() {
		time.Sleep(50 * time.Millisecond)
		hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 123, RecvTimestamp: timestamppb.Now()})
	}()

	sc := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !sc.Scan() {
			break
		}
		if strings.HasPrefix(sc.Text(), "data: ") && strings.Contains(sc.Text(), "car_speed") {
			return // success: got a data event carrying the frame
		}
	}
	t.Fatal("no SSE data event with frame received")
}
