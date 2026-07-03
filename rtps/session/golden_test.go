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
// Detector with default tuning and asserts the ground truth: exactly 4 sessions, race 3 abandoned
// (quit at lap 1 of 10) and the other three finished. This is the end-to-end validation of the
// boundary logic against real GT7 telemetry (D-B4/D-B8).
func TestDetector_Golden4Race(t *testing.T) {
	frames := loadGolden(t)
	if len(frames) < 30000 {
		t.Fatalf("golden looks truncated: %d frames", len(frames))
	}

	sink, store := feed(Config{}, frames...) // default ~0.5s windows
	got := store.List()

	if len(got) != 4 {
		for i, s := range got {
			t.Logf("session %d: car=%d laps=%d/%d frames=%d status=%s dur=%s",
				i, s.CarID, s.CurrentLap, s.TotalLaps, s.FrameCount, s.Status, s.EndTime.Sub(s.StartTime))
		}
		t.Fatalf("got %d sessions, want 4", len(got))
	}

	var abandoned int
	for _, s := range got {
		if s.Status == StatusAbandoned {
			abandoned++
			if s.TotalLaps != 10 {
				t.Errorf("abandoned session TotalLaps=%d, want 10 (race 3)", s.TotalLaps)
			}
			if s.CurrentLap >= s.TotalLaps {
				t.Errorf("abandoned session curLap=%d >= total=%d, contradiction", s.CurrentLap, s.TotalLaps)
			}
		}
	}
	if abandoned != 1 {
		t.Errorf("abandoned sessions = %d, want 1 (race 3 quit)", abandoned)
	}

	// Sanity: every session confirmed => one OnSessionStart/End each, and none is a sub-second blip.
	if len(sink.starts) != 4 || len(sink.ends) != 4 {
		t.Errorf("sink starts/ends = %d/%d, want 4/4", len(sink.starts), len(sink.ends))
	}
}
