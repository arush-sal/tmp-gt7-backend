package processing

import (
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/session"
)

// testDominantTracksContract is the shared Mem/Postgres contract for DominantTracks. Sessions
// "d1".."d4" must exist (Postgres FK).
func testDominantTracksContract(t *testing.T, s LapStore) {
	t.Helper()
	sec := time.Second
	for _, l := range []*Lap{
		// d1: majority wins (suzuka x2 vs interlagos x1); provisional laps never count.
		refLap("d1", 1, 0, 90*sec, true, "interlagos", ConfidenceConfident),
		refLap("d1", 2, 100, 90*sec, true, "suzuka", ConfidenceConfident),
		refLap("d1", 3, 200, 90*sec, true, "suzuka", ConfidenceConfident),
		refLap("d1", 4, 300, 90*sec, true, "brands-hatch-indy", ConfidenceProvisional),
		refLap("d1", 5, 400, 90*sec, true, "brands-hatch-indy", ConfidenceProvisional),
		refLap("d1", 6, 500, 90*sec, true, "brands-hatch-indy", ConfidenceProvisional),
		// d2: a tie; the track of the earliest confident lap wins (lap numbers inserted out of order).
		refLap("d2", 2, 100, 90*sec, true, "suzuka", ConfidenceConfident),
		refLap("d2", 1, 0, 90*sec, true, "interlagos", ConfidenceConfident),
		// d5: a FULL tie (equal counts, same earliest start): the alphabetically first slug wins, so the
		// result is deterministic and identical across the memory and Postgres stores.
		refLap("d5", 1, 0, 90*sec, true, "suzuka", ConfidenceConfident),
		refLap("d5", 2, 0, 90*sec, true, "interlagos", ConfidenceConfident),
		// d3: only provisional / unlabelled laps -> absent.
		refLap("d3", 1, 0, 90*sec, true, "suzuka", ConfidenceProvisional),
		refLap("d3", 2, 100, 90*sec, true, "", ""),
	} {
		if err := s.Save(l); err != nil {
			t.Fatal(err)
		}
	}
	// d4 has no laps at all.
	got := s.DominantTracks()
	want := map[string]string{"d1": "suzuka", "d2": "interlagos", "d5": "interlagos"}
	if len(got) != len(want) {
		t.Fatalf("DominantTracks = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("DominantTracks[%s] = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}

func TestMemLapStore_DominantTracks(t *testing.T) { testDominantTracksContract(t, NewMemLapStore()) }

func TestPostgresLapStore_DominantTracks(t *testing.T) {
	pool := freshPool(t)
	sessions := session.NewPostgresStore(pool)
	for _, id := range []string{"d1", "d2", "d3", "d4", "d5"} {
		if err := sessions.Save(&session.Record{ID: id, StartTime: time.Unix(1000, 0).UTC(), Status: session.StatusFinished}); err != nil {
			t.Fatalf("save session: %v", err)
		}
	}
	testDominantTracksContract(t, NewPostgresLapStore(pool))
}
