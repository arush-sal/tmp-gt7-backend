package live_test

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/analysis"
	"github.com/arush-sal/garage/backend/rtps/ingest"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/reference"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// loadPosGolden reads a reduced, gzipped real capture (decoded via the collector's protocol path by
// decode-harness) that carries the position + last_lap fields the trace/sector path needs. Columns:
// recv_ts_ns,package_id,car_id,current_lap,total_laps,in_race,last_lap_ms,posx,posy,posz. A negative
// last_lap_ms is GT7's "no time" sentinel and is left unset (mirrors the collector's ToCanonical).
//
// Copied verbatim (package renamed only) from rtps/analysis/pipeline_golden_test.go:31.
func loadPosGolden(t *testing.T, path string) []*telemetryv1.TelemetryFrame {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open golden: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer func() { _ = gz.Close() }()

	r := csv.NewReader(gz)
	if _, err := r.Read(); err != nil { // header
		t.Fatalf("header: %v", err)
	}
	var frames []*telemetryv1.TelemetryFrame
	var bestSoFar time.Duration // running GT7-style best_lap; the golden CSV has no best_lap column
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("row: %v", err)
		}
		fr := &telemetryv1.TelemetryFrame{
			RecvTimestamp: timestamppb.New(time.Unix(0, mi64(t, row[0]))),
			PackageId:     uint32(mi64(t, row[1])),
			CarId:         int32(mi64(t, row[2])),
			CurrentLap:    int32(mi64(t, row[3])),
			TotalLaps:     int32(mi64(t, row[4])),
			InRace:        row[5] == "true",
			PositionX:     f32(t, row[7]),
			PositionY:     f32(t, row[8]),
			PositionZ:     f32(t, row[9]),
			Source:        telemetryv1.Source_SOURCE_LIVE,
		}
		// The CSV predates the detector's must-move rule and has no speed column; its in-race frames
		// are real racing, so mark them moving (others stay at 0 km/h).
		if fr.InRace {
			fr.CarSpeed = 100
		}
		if ms := mi64(t, row[6]); ms >= 0 { // negative = GT7 "no lap time" sentinel -> leave nil
			d := time.Duration(ms) * time.Millisecond
			fr.LastLap = durationpb.New(d)
			// Mirror GT7's own best_lap tracking so this synthesized stream exercises the analyzer's
			// gt7Best threading the same way a real capture would (the CSV columns predate best_lap,
			// so it isn't in the source data — derive it as the running min of completed lap times).
			if bestSoFar == 0 || d < bestSoFar {
				bestSoFar = d
			}
		}
		if bestSoFar > 0 {
			fr.BestLap = durationpb.New(bestSoFar)
		}
		frames = append(frames, fr)
	}
	return frames
}

func mi64(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("parse int %q: %v", s, err)
	}
	return v
}

func f32(t *testing.T, s string) float32 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		t.Fatalf("parse float %q: %v", s, err)
	}
	return float32(v)
}

// mustLatest adapts live.Hub.Latest()'s 3-tuple to the (frame, lapStart, ok) triple needed alongside
// the current live enrichment snapshot to call live.BuildPayload's 4-arg signature.
//
// NOTE: the task brief's original sketch had this helper hardcode a zero-value live.Enrichment{},
// which would silently discard the real enrichment state (defeating the point of this e2e test, which
// exists specifically to prove that enrichment populates and flows into the payload). That is a defect
// in the brief; this helper instead returns the frame/lapStart/ok tuple only, and the caller pairs it
// with a live enrich.Get() call, matching BuildPayload's actual 4-arg signature.
func mustLatest(h *live.Hub) (*telemetryv1.TelemetryFrame, time.Time, bool) {
	return h.Latest()
}

// TestEgress_GoldenReplayPopulatesLive proves the full real-time egress pipeline end-to-end: a real
// capture streamed through gRPC ingest (with the live.Hub publisher wired in) flows through the
// Detector, then fans out to the lap segmenter, trace recorder, and enrichment sink (Processing ->
// Analysis), exactly as internal/app/backend.go wires it. It asserts direct metrics are visible on the
// live snapshot immediately (publish-first egress) and that enrichment.analysis populates once the
// async enrichment worker has processed a completed lap boundary.
func TestEgress_GoldenReplayPopulatesLive(t *testing.T) {
	frames := loadPosGolden(t, "../analysis/testdata/analysis_golden_race1.csv.gz")
	if len(frames) == 0 {
		t.Fatal("golden loaded zero frames")
	}

	// Wire the pipeline exactly like internal/app/backend.go (in-memory stores), INCLUDING the
	// AsyncTraceStore wrapper around the trace store, so this test exercises the same async
	// storage-write path production does rather than a bare synchronous MemTraceStore.
	sessStore := session.NewMemStore()
	lapStore := processing.NewMemLapStore()
	traceStore := live.NewAsyncTraceStore(processing.NewMemTraceStore(), 256, nil)
	defer traceStore.Close()
	hub := live.New()
	enrich := live.NewEnrichmentState()
	analyzer := analysis.Analyzer{Laps: lapStore, Traces: traceStore, KB: reference.NewStaticKB(), FallbackSectors: 3}
	enrichSink := live.NewEnrichSink(analyzer, enrich, nil)
	defer enrichSink.Close() // stop the worker goroutine so the test doesn't leak it

	sink := session.NewMultiSink(
		processing.NewLapSegmenter(lapStore),
		processing.NewTraceRecorder(traceStore),
		enrichSink,
	)

	srv := ingest.NewServer(sessStore, sink, session.Config{}, zap.NewNop())
	srv.Publisher = hub // publish-first egress

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	telemetryv1.RegisterTelemetryServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	defer gs.Stop()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	stream, err := telemetryv1.NewTelemetryClient(conn).Ingest(context.Background())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	for _, f := range frames {
		if err := stream.Send(f); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	// Direct metrics available from the latest frame (publish-first: no analysis dependency).
	if _, _, ok := hub.Latest(); !ok {
		t.Fatal("hub has no frame after replay")
	}

	// Assert enrichment BEFORE CloseAndRecv, while the session is still open, to prove it populates
	// during the active session (not only as a leftover). The server processes sent frames
	// asynchronously, so poll with a bounded timeout rather than sleeping a fixed duration or racing
	// the assertion.
	var preCloseSessionID string
	deadline := time.Now().Add(2 * time.Second)
	for {
		if en := enrich.Get(); en.Analysis != nil && en.Analysis.HasPB {
			preCloseSessionID = en.SessionID
			frame, lapStart, ok := mustLatest(hub)
			body, err := live.BuildPayload(frame, lapStart, ok, en)
			if err != nil {
				t.Fatalf("payload: %v", err)
			}
			// Prove live egress genuinely reached the wire payload: BuildPayload always emits a
			// "frame" key (null when idle), so merely checking key presence is vacuous. Instead
			// assert status is "live", the frame is non-null, and enrichment.analysis.has_pb
			// carried through to the marshalled payload.
			var m map[string]json.RawMessage
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			var status string
			if err := json.Unmarshal(m["status"], &status); err != nil {
				t.Fatalf("unmarshal status: %v", err)
			}
			if status != "live" {
				t.Errorf("status = %q, want %q", status, "live")
			}
			if string(m["frame"]) == "null" {
				t.Error("payload frame is null, want a live frame")
			}
			var enr struct {
				Analysis *struct {
					HasPB bool `json:"has_pb"`
				} `json:"analysis"`
			}
			if err := json.Unmarshal(m["enrichment"], &enr); err != nil {
				t.Fatalf("unmarshal enrichment: %v", err)
			}
			if enr.Analysis == nil || !enr.Analysis.HasPB {
				t.Error("payload enrichment.analysis.has_pb not true")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("enrichment analysis never populated after replay")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}

	// Lock in the race-lifecycle A1 requirement: once the session ends (EOF -> OnSessionEnd), the
	// finished race's live enrichment is RETAINED (not cleared), so post-race analysis still has
	// track/session context. Poll for a sustained window to prove it isn't cleared a moment later by
	// some other path.
	sustainedUntil := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(sustainedUntil) {
		en := enrich.Get()
		if en.Analysis == nil || !en.Analysis.HasPB {
			t.Fatalf("enrichment was cleared after session end, want retained: %+v", en)
		}
		if en.SessionID != preCloseSessionID {
			t.Fatalf("SessionID after session end = %q, want retained %q", en.SessionID, preCloseSessionID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
