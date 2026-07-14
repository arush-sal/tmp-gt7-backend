package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// trackEvent is the parsed payload of an SSE "event: track" frame.
type trackEvent struct {
	Slug       *string     `json:"slug"`
	Confidence *string     `json:"confidence"`
	Record     *rkb.Record `json:"record"`
}

// sseLine is one parsed SSE frame: the event name (default "message") and its data payload.
type sseLine struct {
	event string
	data  string
}

// readSSE scans resp.Body for SSE frames until deadline, calling stop(line) after each; stop returns
// true to end the scan early. It returns all frames observed.
func readSSE(t *testing.T, body *bufio.Scanner, deadline time.Time, stop func(sseLine) bool) []sseLine {
	t.Helper()
	var lines []sseLine
	curEvent := ""
	for time.Now().Before(deadline) {
		if !body.Scan() {
			break
		}
		text := body.Text()
		switch {
		case strings.HasPrefix(text, "event: "):
			curEvent = strings.TrimPrefix(text, "event: ")
		case strings.HasPrefix(text, "data: "):
			line := sseLine{event: curEvent, data: strings.TrimPrefix(text, "data: ")}
			if curEvent == "" {
				line.event = "message"
			}
			lines = append(lines, line)
			curEvent = ""
			if stop(line) {
				return lines
			}
		}
	}
	return lines
}

func newTrackTestServer() (*Server, *live.Hub, *live.EnrichmentState) {
	hub := live.New()
	enrich := live.NewEnrichmentState()
	store := fakeTracks{
		byID: map[string]*rkb.Record{"hsr": {Slug: "hsr", Name: "High Speed Ring"}},
	}
	srv := NewServer(coach.Coach{}, nil, hub, enrich, nil, store, nil)
	return srv, hub, enrich
}

func connectSSE(t *testing.T, ts *httptest.Server) (*http.Response, *bufio.Scanner, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/live/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v", err)
	}
	return resp, bufio.NewScanner(resp.Body), cancel
}

// (a) subscriber connected BEFORE resolution: receives exactly one track event once resolved; a
// second frame publish with the same slug does not re-emit.
func TestLiveStream_TrackEvent_EmitsOnceOnResolution(t *testing.T) {
	srv, hub, enrich := newTrackTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, sc, cancel := connectSSE(t, ts)
	defer cancel()
	defer func() { _ = resp.Body.Close() }()

	// give the subscriber goroutine a moment to register.
	time.Sleep(50 * time.Millisecond)

	slug := "hsr"
	conf := "confident"
	enrich.SetTrack("sess-1", &slug, &conf)
	hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 1, RecvTimestamp: timestamppb.Now()})

	var trackEvents []sseLine
	deadline := time.Now().Add(2 * time.Second)
	lines := readSSE(t, sc, deadline, func(l sseLine) bool {
		if l.event == "track" {
			trackEvents = append(trackEvents, l)
		}
		return len(trackEvents) >= 1
	})
	if len(trackEvents) != 1 {
		t.Fatalf("got %d track events (lines=%+v), want 1", len(trackEvents), lines)
	}
	var ev trackEvent
	if err := json.Unmarshal([]byte(trackEvents[0].data), &ev); err != nil {
		t.Fatalf("unmarshal track event: %v", err)
	}
	if ev.Slug == nil || *ev.Slug != "hsr" {
		t.Errorf("slug = %v, want hsr", ev.Slug)
	}
	if ev.Confidence == nil || *ev.Confidence != "confident" {
		t.Errorf("confidence = %v, want confident", ev.Confidence)
	}
	if ev.Record == nil || ev.Record.Slug != "hsr" {
		t.Errorf("record = %+v, want slug hsr", ev.Record)
	}

	// publish a second frame with the SAME slug: no additional track event should appear.
	hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 2, RecvTimestamp: timestamppb.Now()})
	extra := readSSE(t, sc, time.Now().Add(300*time.Millisecond), func(l sseLine) bool {
		return l.event == "track"
	})
	for _, l := range extra {
		if l.event == "track" {
			t.Fatalf("unexpected repeat track event: %+v", l)
		}
	}
}

// (b) subscriber connecting AFTER resolution gets the track event immediately (replay-on-connect).
func TestLiveStream_TrackEvent_ReplayOnConnect(t *testing.T) {
	srv, _, enrich := newTrackTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	slug := "hsr"
	conf := "confident"
	enrich.SetTrack("sess-1", &slug, &conf)

	resp, sc, cancel := connectSSE(t, ts)
	defer cancel()
	defer func() { _ = resp.Body.Close() }()

	deadline := time.Now().Add(2 * time.Second)
	var got *trackEvent
	readSSE(t, sc, deadline, func(l sseLine) bool {
		if l.event == "track" {
			var ev trackEvent
			if err := json.Unmarshal([]byte(l.data), &ev); err == nil {
				got = &ev
			}
			return true
		}
		return false
	})
	if got == nil {
		t.Fatal("no track event received on initial connect")
	}
	if got.Slug == nil || *got.Slug != "hsr" {
		t.Errorf("slug = %v, want hsr", got.Slug)
	}
	if got.Record == nil || got.Record.Slug != "hsr" {
		t.Errorf("record = %+v, want slug hsr", got.Record)
	}
}

// (c) slug change re-emits a new track event.
func TestLiveStream_TrackEvent_ReemitsOnSlugChange(t *testing.T) {
	srv, hub, enrich := newTrackTestServer()
	// add a second track to the fake store.
	srv.tracks = fakeTracks{
		byID: map[string]*rkb.Record{
			"hsr": {Slug: "hsr", Name: "High Speed Ring"},
			"btc": {Slug: "btc", Name: "Brands Hatch"},
		},
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, sc, cancel := connectSSE(t, ts)
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	time.Sleep(50 * time.Millisecond)

	slug1 := "hsr"
	enrich.SetTrack("sess-1", &slug1, nil)
	hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 1, RecvTimestamp: timestamppb.Now()})

	var firstSlug string
	readSSE(t, sc, time.Now().Add(2*time.Second), func(l sseLine) bool {
		if l.event == "track" {
			var ev trackEvent
			_ = json.Unmarshal([]byte(l.data), &ev)
			if ev.Slug != nil {
				firstSlug = *ev.Slug
			}
			return true
		}
		return false
	})
	if firstSlug != "hsr" {
		t.Fatalf("first slug = %q, want hsr", firstSlug)
	}

	slug2 := "btc"
	enrich.SetTrack("sess-1", &slug2, nil)
	hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 2, RecvTimestamp: timestamppb.Now()})

	var secondSlug string
	readSSE(t, sc, time.Now().Add(2*time.Second), func(l sseLine) bool {
		if l.event == "track" {
			var ev trackEvent
			_ = json.Unmarshal([]byte(l.data), &ev)
			if ev.Slug != nil {
				secondSlug = *ev.Slug
			}
			return true
		}
		return false
	})
	if secondSlug != "btc" {
		t.Fatalf("second slug = %q, want btc", secondSlug)
	}
}

// (d) ClearTrack emits a track event with slug null.
func TestLiveStream_TrackEvent_ClearEmitsNullSlug(t *testing.T) {
	srv, hub, enrich := newTrackTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, sc, cancel := connectSSE(t, ts)
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	time.Sleep(50 * time.Millisecond)

	slug := "hsr"
	enrich.SetTrack("sess-1", &slug, nil)
	hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 1, RecvTimestamp: timestamppb.Now()})
	readSSE(t, sc, time.Now().Add(2*time.Second), func(l sseLine) bool { return l.event == "track" })

	enrich.ClearTrack("sess-1")
	hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 2, RecvTimestamp: timestamppb.Now()})

	var got *trackEvent
	readSSE(t, sc, time.Now().Add(2*time.Second), func(l sseLine) bool {
		if l.event == "track" {
			var ev trackEvent
			if err := json.Unmarshal([]byte(l.data), &ev); err == nil {
				got = &ev
			}
			return true
		}
		return false
	})
	if got == nil {
		t.Fatal("no track event received after ClearTrack")
	}
	if got.Slug != nil {
		t.Errorf("slug = %v, want nil", *got.Slug)
	}
}

// (e) frame data: events remain slug-only; they never carry the full record.
func TestLiveStream_FrameEvents_StayRecordFree(t *testing.T) {
	srv, hub, enrich := newTrackTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, sc, cancel := connectSSE(t, ts)
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	time.Sleep(50 * time.Millisecond)

	slug := "hsr"
	enrich.SetTrack("sess-1", &slug, nil)
	hub.Publish(&telemetryv1.TelemetryFrame{CarSpeed: 42, RecvTimestamp: timestamppb.Now()})

	var sawFrame bool
	readSSE(t, sc, time.Now().Add(2*time.Second), func(l sseLine) bool {
		if l.event == "message" && strings.Contains(l.data, "car_speed") {
			sawFrame = true
			if strings.Contains(l.data, "High Speed Ring") {
				t.Fatalf("frame data event leaked the full record: %s", l.data)
			}
		}
		return sawFrame
	})
	if !sawFrame {
		t.Fatal("no frame data event observed")
	}
}
