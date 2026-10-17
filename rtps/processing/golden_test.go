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
		// The fixture predates the must-move rule and has no speed column; its in-race frames are all
		// real racing, so mark them moving (others stay at 0 km/h).
		if fr.InRace {
			fr.CarSpeed = 100
		}
		if ms := atoi(row[8]); ms > 0 {
			fr.LastLap = durationpb.New(time.Duration(ms) * time.Millisecond)
		}
		frames = append(frames, fr)
	}
	return frames
}

func atoi(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }

// goldenNewID reproduces the detector's historical monotonic "sess-N" id generator (its pre-UUID
// default). The production default now mints UUIDs (uuid.NewString), so these golden tests must pin a
// deterministic generator to keep the expectation maps keyed on sess-N stable. Races 1-4 are sess-1..4
// (the finish-line results flap no longer opens sessions: lap-0 frames never split); sess-3, race 3,
// has no completed timed lap and is discarded.
func goldenNewID() func() string {
	n := 0
	return func() string { n++; return "sess-" + strconv.Itoa(n) }
}

// TestSegmenter_Golden4Race runs the real capture through Detector -> LapSegmenter and reports the
// per-session lap breakdown, then asserts structural invariants.
func TestSegmenter_Golden4Race(t *testing.T) {
	frames := loadGoldenFrames(t)

	lapStore := NewMemLapStore()
	seg := NewLapSegmenter(lapStore)
	sessStore := session.NewMemStore()
	d := session.NewDetector(seg, sessStore, session.Config{NewID: goldenNewID()})
	for _, f := range frames {
		d.Observe(f)
	}
	d.Close()

	sessions := sessStore.List()
	if len(sessions) != 3 {
		t.Fatalf("sessions=%d, want 3 (race 3 has no completed lap and is discarded)", len(sessions))
	}

	// Expected per-session (laps, valid) derived from the capture: the two 2-lap races segment to
	// [0,1,2,3] with laps 1 and 2 valid; the 1-lap race is [0,1,2] with lap 1 valid. The abandoned
	// 10-lap quit (sess-3) never completed a timed lap and is discarded, so it is not stored.
	wantLaps := map[string][2]int{
		"sess-1": {4, 2},
		"sess-2": {4, 2},
		"sess-4": {3, 1},
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
