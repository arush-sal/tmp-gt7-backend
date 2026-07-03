package processing

import (
	"compress/gzip"
	"encoding/csv"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/session"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// loadGoldenFrames reads the shared 4-race golden (session testdata) including the last_lap column,
// as canonical frames in stream order.
func loadGoldenFrames(t *testing.T) []*telemetryv1.TelemetryFrame {
	t.Helper()
	f, err := os.Open("../session/testdata/sm_golden_4race.csv.gz")
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
	if _, err := r.Read(); err != nil {
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
		// idx,recv_ts_ns,package_id,car_id,current_lap,total_laps,in_race,is_paused,last_lap_ms
		fr := &telemetryv1.TelemetryFrame{
			RecvTimestamp: timestamppb.New(time.Unix(0, atoi(row[1]))),
			PackageId:     uint32(atoi(row[2])),
			CarId:         int32(atoi(row[3])),
			CurrentLap:    int32(atoi(row[4])),
			TotalLaps:     int32(atoi(row[5])),
			InRace:        row[6] == "true",
			Source:        telemetryv1.Source_SOURCE_LIVE,
		}
		if ms := atoi(row[8]); ms > 0 {
			fr.LastLap = durationpb.New(time.Duration(ms) * time.Millisecond)
		}
		frames = append(frames, fr)
	}
	return frames
}

func atoi(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }

// TestSegmenter_Golden4Race runs the real capture through Detector -> LapSegmenter and reports the
// per-session lap breakdown, then asserts structural invariants.
func TestSegmenter_Golden4Race(t *testing.T) {
	frames := loadGoldenFrames(t)

	lapStore := NewMemLapStore()
	seg := NewLapSegmenter(lapStore)
	sessStore := session.NewMemStore()
	d := session.NewDetector(seg, sessStore, session.Config{})
	for _, f := range frames {
		d.Observe(f)
	}
	d.Close()

	sessions := sessStore.List()
	if len(sessions) != 4 {
		t.Fatalf("sessions=%d, want 4", len(sessions))
	}

	// Expected per-session (laps, valid) derived from the capture: the two 2-lap races segment to
	// [0,1,2,3] with laps 1 and 2 valid; the abandoned 10-lap quit stops at [0,1] with none valid; the
	// 1-lap race is [0,1,2] with lap 1 valid.
	wantLaps := map[string][2]int{
		"sess-1": {4, 2},
		"sess-3": {4, 2},
		"sess-5": {2, 0}, // abandoned (quit at lap 1) -> no completed timed lap
		"sess-6": {3, 1},
	}

	for _, s := range sessions {
		laps := lapStore.BySession(s.ID)
		var valid int
		for i, l := range laps {
			if l.Valid {
				valid++
			}
			// Invariant: lap numbers are contiguous from 0.
			if l.LapNumber != int32(i) {
				t.Errorf("%s lap[%d].LapNumber = %d, want %d (contiguous from 0)", s.ID, i, l.LapNumber, i)
			}
			// Invariant: only the final lap is incomplete (session ended mid/at cooldown lap).
			wantComplete := i < len(laps)-1
			if l.Complete != wantComplete {
				t.Errorf("%s lap %d Complete = %v, want %v", s.ID, l.LapNumber, l.Complete, wantComplete)
			}
			// A valid lap must have a positive duration.
			if l.Valid && l.Duration <= 0 {
				t.Errorf("%s lap %d valid but Duration=%s", s.ID, l.LapNumber, l.Duration)
			}
		}
		want, ok := wantLaps[s.ID]
		if !ok {
			t.Errorf("unexpected session id %s", s.ID)
			continue
		}
		if len(laps) != want[0] || valid != want[1] {
			t.Errorf("%s: %d laps / %d valid, want %d / %d", s.ID, len(laps), valid, want[0], want[1])
		}
		// The abandoned session must have no PB-usable lap.
		if s.Status == session.StatusAbandoned && valid != 0 {
			t.Errorf("abandoned %s has %d valid laps, want 0", s.ID, valid)
		}
	}
}
