package processing

import (
	"fmt"
	"testing"
	"time"
)

// trackRefStore is what the contract test drives: the lap store plus its track-reference methods.
type trackRefStore interface {
	LapStore
	TrackRefStore
}

// at returns a fixed instant offset by sec seconds (laps are ordered by start time).
func at(sec int) time.Time { return time.Unix(int64(10000+sec), 0).UTC() }

// refLap builds a lap; valid laps are also timed + complete (as the segmenter emits them).
func refLap(session string, n int32, start int, dur time.Duration, valid bool, slug, conf string) *Lap {
	return &Lap{SessionID: session, LapNumber: n, StartTime: at(start), EndTime: at(start).Add(dur), Duration: dur,
		Timed: true, Complete: valid, Valid: valid, TrackSlug: slug, TrackConfidence: conf}
}

// testTrackRefContract is the shared Mem/Postgres contract for per-lap tracks, the fastest-lap query,
// pins (incl. the cascade when the pinned lap's session goes) and reference resolution. dropSession
// removes a whole session the way production does (the Postgres session row cascade, or the lap
// store's DeleteSession for memory).
func testTrackRefContract(t *testing.T, s trackRefStore, dropSession func(id string)) {
	t.Helper()
	sec := time.Second
	for _, l := range []*Lap{
		refLap("s1", 1, 0, 100*sec, true, "suzuka", ConfidenceConfident),
		refLap("s1", 2, 100, 98*sec, true, "suzuka", ConfidenceConfident),
		refLap("s1", 3, 200, 90*sec, true, "suzuka", ConfidenceProvisional), // provisional: not eligible yet
		refLap("s1", 4, 300, 80*sec, false, "suzuka", ConfidenceConfident),  // invalid: never eligible
		refLap("s1", 5, 400, 95*sec, true, "", ""),                          // unlabelled
		refLap("s1", 6, 500, 85*sec, true, "other", ConfidenceConfident),    // another stint's lock: kept
		refLap("s2", 1, 1000, 70*sec, true, "red-bull-ring", ConfidenceConfident),
		refLap("s2", 2, 1100, 99*sec, true, "suzuka", ConfidenceConfident),
	} {
		if err := s.Save(l); err != nil {
			t.Fatalf("save %s#%d: %v", l.SessionID, l.LapNumber, err)
		}
	}

	if got := s.BySession("s1"); got[2].TrackSlug != "suzuka" || got[2].TrackConfidence != ConfidenceProvisional ||
		got[4].TrackSlug != "" || got[4].TrackConfidence != "" {
		t.Fatalf("track fields round-trip: %+v / %+v", got[2], got[4])
	}

	laps, err := s.TrackLaps("suzuka", 10)
	if err != nil {
		t.Fatal(err)
	}
	assertLapIDs(t, "fastest suzuka", laps, "s1#2", "s2#2", "s1#1")
	if laps, _ := s.TrackLaps("suzuka", 2); len(laps) != 2 {
		t.Errorf("limit 2 -> %d laps", len(laps))
	}

	// Lock on suzuka for the stint that began at lap 3: laps 3 (provisional) and 5 (unlabelled) become
	// confident; lap 6 (locked on another track) and laps before the stint stay as they were.
	if err := s.LabelTrack("s1", at(200), "suzuka"); err != nil {
		t.Fatal(err)
	}
	got := s.BySession("s1")
	for i, want := range []string{"suzuka", "suzuka", "suzuka", "suzuka", "suzuka", "other"} {
		if got[i].TrackSlug != want || got[i].TrackConfidence != ConfidenceConfident {
			t.Errorf("after lock lap %d = %q/%q, want %q/confident", got[i].LapNumber, got[i].TrackSlug, got[i].TrackConfidence, want)
		}
	}
	laps, _ = s.TrackLaps("suzuka", 10)
	assertLapIDs(t, "after lock", laps, "s1#3", "s1#5", "s1#2", "s2#2", "s1#1")

	if l, ok, err := s.Lap("s1", 2); err != nil || !ok || l.Duration != 98*sec {
		t.Errorf("Lap(s1,2) = %+v %v %v", l, ok, err)
	}
	if _, ok, err := s.Lap("s1", 99); err != nil || ok {
		t.Errorf("Lap(s1,99) found=%v err=%v, want not found", ok, err)
	}

	ref, ok, err := ResolveTrackReference(s, "suzuka")
	if err != nil || !ok || ref.Source != RefSourceFastest || lapID(&ref.Lap) != "s1#3" {
		t.Fatalf("resolve unpinned = %+v %v %v, want fastest s1#3", ref, ok, err)
	}
	if _, ok, _ := ResolveTrackReference(s, "nowhere"); ok {
		t.Error("resolve unknown track: want not found")
	}

	if err := s.PinReference("suzuka", "s1", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.PinReference("suzuka", "s2", 2); err != nil { // re-pin replaces
		t.Fatal(err)
	}
	if p, ok, err := s.PinnedReference("suzuka"); err != nil || !ok || lapID(p) != "s2#2" {
		t.Fatalf("pinned = %v %v %v, want s2#2", p, ok, err)
	}
	ref, _, _ = ResolveTrackReference(s, "suzuka")
	if ref.Source != RefSourcePinned || lapID(&ref.Lap) != "s2#2" {
		t.Errorf("resolve pinned = %+v", ref)
	}
	if err := s.PinReference("suzuka", "s1", 99); err == nil {
		t.Error("pin of a missing lap: want error")
	}

	// Dropping the pinned lap's session removes the pin: the track falls back to its fastest lap.
	dropSession("s2")
	if _, ok, _ := s.PinnedReference("suzuka"); ok {
		t.Error("pin survived its session's deletion")
	}
	ref, _, _ = ResolveTrackReference(s, "suzuka")
	if ref.Source != RefSourceFastest || lapID(&ref.Lap) != "s1#3" {
		t.Errorf("after cascade = %+v, want fastest s1#3", ref)
	}

	if err := s.PinReference("suzuka", "s1", 2); err != nil {
		t.Fatal(err)
	}
	if removed, err := s.UnpinReference("suzuka"); err != nil || !removed {
		t.Errorf("unpin = %v %v, want removed", removed, err)
	}
	if removed, err := s.UnpinReference("suzuka"); err != nil || removed {
		t.Errorf("second unpin = %v %v, want nothing removed", removed, err)
	}
}

func lapID(l *Lap) string {
	if l == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s#%d", l.SessionID, l.LapNumber)
}

func assertLapIDs(t *testing.T, what string, laps []*Lap, want ...string) {
	t.Helper()
	if len(laps) != len(want) {
		t.Errorf("%s: %d laps, want %v", what, len(laps), want)
		return
	}
	for i, l := range laps {
		if lapID(l) != want[i] {
			t.Errorf("%s[%d] = %s, want %s", what, i, lapID(l), want[i])
		}
	}
}

func TestMemLapStore_TrackReference(t *testing.T) {
	s := NewMemLapStore()
	testTrackRefContract(t, s, func(id string) { _ = s.DeleteSession(id) })
}
