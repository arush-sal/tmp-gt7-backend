package session

import (
	"compress/gzip"
	"encoding/csv"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// loadGolden reads the frozen 4-race capture (decoded via the collector's protocol path, then reduced
// to the columns the Detector consumes) and returns it as canonical frames in stream order. Provenance
// and the expected outcome are documented in the vault note "2026-07-03 Session Boundary Capture
// Analysis" (D-B8).
func loadGolden(t *testing.T) []*telemetryv1.TelemetryFrame {
	t.Helper()
	f, err := os.Open("testdata/sm_golden_4race.csv.gz")
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
	if _, err := r.Read(); err != nil { // discard header
		t.Fatalf("read header: %v", err)
	}

	var frames []*telemetryv1.TelemetryFrame
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read row: %v", err)
		}
		// columns: idx,recv_ts_ns,package_id,car_id,current_lap,total_laps,in_race,is_paused
		ns := mustInt64(t, row[1])
		frames = append(frames, &telemetryv1.TelemetryFrame{
			RecvTimestamp: timestamppb.New(time.Unix(0, ns)),
			PackageId:     uint32(mustInt64(t, row[2])),
			CarId:         int32(mustInt64(t, row[3])),
			CurrentLap:    int32(mustInt64(t, row[4])),
			TotalLaps:     int32(mustInt64(t, row[5])),
			InRace:        row[6] == "true",
			Source:        telemetryv1.Source_SOURCE_LIVE,
		})
		// The fixture predates the must-move rule and carries no speed column. Every in-race frame of
		// this capture is a real racing stint, so mark those as moving; others stay at 0 km/h.
		if row[6] == "true" {
			frames[len(frames)-1].CarSpeed = 100
		}
	}
	return frames
}

func mustInt64(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

// TestDetector_Golden4Race runs the whole real capture (game start -> 4 races -> quit) through the
// Detector with default tuning and asserts the ground truth: 4 races are detected (4 confirmed sessions
// reach the sinks), race 3 is the quit at lap 1 of 10 and tagged abandoned, the other three finished.
// Race 3 never completed a timed lap, so it is discarded (DiscardNoCompletedLap) instead of stored:
// exactly 3 sessions persist. This is the end-to-end validation of the boundary logic against real GT7
// telemetry (D-B4/D-B8).
func TestDetector_Golden4Race(t *testing.T) {
	frames := loadGolden(t)
	if len(frames) < 30000 {
		t.Fatalf("golden looks truncated: %d frames", len(frames))
	}

	var discarded []*Record
	cfg := Config{OnDiscard: func(r *Record, reason DiscardReason, _ string) {
		if reason != DiscardNoCompletedLap {
			t.Errorf("unexpected discard reason %q", reason)
		}
		c := *r
		discarded = append(discarded, &c)
	}}
	sink, store := feed(cfg, frames...) // default ~0.5s windows
	got := store.List()

	if len(got) != 3 {
		for i, s := range got {
			t.Logf("session %d: car=%d laps=%d/%d frames=%d status=%s dur=%s",
				i, s.CarID, s.CurrentLap, s.TotalLaps, s.FrameCount, s.Status, s.EndTime.Sub(s.StartTime))
		}
		t.Fatalf("got %d stored sessions, want 3 (races 1, 2, 4; race 3 has no completed lap)", len(got))
	}
	for _, s := range got {
		if s.Status != StatusFinished {
			t.Errorf("stored session %s status=%s, want finished", s.ID, s.Status)
		}
	}
	if len(discarded) != 1 {
		t.Fatalf("discarded = %d sessions, want 1 (race 3)", len(discarded))
	}
	if r := discarded[0]; r.Status != StatusAbandoned || r.TotalLaps != 10 || r.CurrentLap != 1 {
		t.Errorf("race 3 = status %s lap %d/%d, want abandoned at lap 1/10", r.Status, r.CurrentLap, r.TotalLaps)
	}

	// Sanity: every session confirmed => one OnSessionStart/End each, and none is a sub-second blip.
	if len(sink.starts) != 4 || len(sink.ends) != 4 {
		t.Errorf("sink starts/ends = %d/%d, want 4/4", len(sink.starts), len(sink.ends))
	}
}
