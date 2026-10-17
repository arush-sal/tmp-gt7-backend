package analysis

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/ingest"
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
			fr.LastLap = durationpb.New(time.Duration(ms) * time.Millisecond)
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

// TestPipeline_GoldenSectorsReconcile is the end-to-end guard for the sector-analyzability fix. It
// streams a real one-race capture (formation lap, a standing-start lap 1, a clean flying lap 2, a
// partial final lap) through the WHOLE backend seam over a real gRPC client stream — ingest ->
// Detector -> LapSegmenter + TraceRecorder -> stores -> Analyzer — exactly as the collector drives it.
//
// The standing-start lap 1's trace spans ~96.9s of wall clock while GT7 times the lap at 88.6s (grid
// launch frames precede the timing line). Before the fix the analyzer trusted that lap's sector times,
// so its per-sector times summed to 96.9s, not its 88.6s duration. The core assertion is the invariant
// that must hold for every reported lap: its sector times reconcile with its authoritative duration.
func TestPipeline_GoldenSectorsReconcile(t *testing.T) {
	frames := loadPosGolden(t, "testdata/analysis_golden_race1.csv.gz")
	if len(frames) < 10000 {
		t.Fatalf("golden looks truncated: %d frames", len(frames))
	}

	// Wire the pipeline exactly like internal/app/backend.go, minus the servers we don't need.
	sessStore := session.NewMemStore()
	lapStore := processing.NewMemLapStore()
	traceStore := processing.NewMemTraceStore()
	sink := session.NewMultiSink(processing.NewLapSegmenter(lapStore), processing.NewTraceRecorder(traceStore))

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	telemetryv1.RegisterTelemetryServer(gs, ingest.NewServer(sessStore, sink, session.Config{}, zap.NewNop()))
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
			t.Fatalf("Send: %v", err)
		}
	}
	if _, err := stream.CloseAndRecv(); err != nil {
		t.Fatalf("CloseAndRecv: %v", err)
	}

	sessions := sessStore.List()
	if len(sessions) != 1 {
		t.Fatalf("detected %d sessions, want 1", len(sessions))
	}
	id := sessions[0].ID
	// This golden's frames don't carry a best_lap field (see loadPosGolden), so there is no GT7-
	// authoritative best to read off the Record here; fall back to the fastest GT7-timed, complete lap
	// the segmenter captured, which is what a real BestLap would equal on a clean capture.
	var gt7Best time.Duration
	for _, l := range lapStore.BySession(id) {
		if l == nil || !l.Timed || !l.Complete {
			continue
		}
		if gt7Best == 0 || l.Duration < gt7Best {
			gt7Best = l.Duration
		}
	}

	a := Analyzer{Laps: lapStore, Traces: traceStore, KB: reference.NewStaticKB(), FallbackSectors: 3}
	res, err := a.AnalyzeSession(context.Background(), id, gt7Best)
	if err != nil {
		t.Fatalf("AnalyzeSession: %v", err)
	}

	// A real PB was found and it is a sane road-course lap time (~73.7s here), not a sentinel.
	if !res.HasPB {
		t.Fatal("no PB detected on a golden with two valid laps")
	}
	if res.PBTime < 30*time.Second || res.PBTime > 5*time.Minute {
		t.Errorf("PB time %v implausible for a race lap", res.PBTime)
	}
	// Predicted best is bounded by the PB (BUG-4 invariant, enforced by suppression) and never collapses
	// to a partial lap.
	if res.PredictedBest <= 0 || res.PredictedBest > res.PBTime {
		t.Errorf("predicted best %v out of range (want 0 < pb <= PB %v)", res.PredictedBest, res.PBTime)
	}
	if res.PredictedBest < res.PBTime/2 {
		t.Errorf("predicted best %v implausibly small vs PB %v — a mis-traced lap leaked in", res.PredictedBest, res.PBTime)
	}

	// THE invariant: for every lap that carries sector deltas, its own per-sector times sum to its
	// authoritative duration. The standing-start lap 1 (trace span != duration) must therefore either
	// be excluded from sector timing (no deltas) or reconcile — never report sectors that contradict
	// its lap time.
	analyzed := 0
	for _, l := range res.Laps {
		if len(l.SectorDeltas) == 0 {
			continue
		}
		analyzed++
		var sum time.Duration
		for _, sd := range l.SectorDeltas {
			sum += sd.LapTime
		}
		if d := absDur(sum - l.Duration); d > spanTol(l.Duration) {
			t.Errorf("lap %d: sector-time sum %v disagrees with duration %v (off by %v)",
				l.LapNumber, sum, l.Duration, d)
		}
	}
	if analyzed == 0 {
		t.Error("no lap produced sector deltas — the clean flying lap should be analyzable")
	}
}
