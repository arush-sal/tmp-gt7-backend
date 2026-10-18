package processing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
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
// store's DeleteSession for memory). setTotal sets a session's total_laps the way the Detector's
// persistence does (the sessions row, or the memory session lookup).
func testTrackRefContract(t *testing.T, s trackRefStore, dropSession func(id string), setTotal func(id string, n int32)) {
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
	if err := s.LabelTrack(context.Background(), "s1", TimeSpan{From: at(200)}, "suzuka"); err != nil {
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

	ref, ok, err := ResolveTrackReference(s, "suzuka", nil)
	if err != nil || !ok || ref.Source != RefSourceFastest || lapID(&ref.Lap) != "s1#3" {
		t.Fatalf("resolve unpinned = %+v %v %v, want fastest s1#3", ref, ok, err)
	}
	if _, ok, _ := ResolveTrackReference(s, "nowhere", nil); ok {
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
	ref, _, _ = ResolveTrackReference(s, "suzuka", nil)
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
	ref, _, _ = ResolveTrackReference(s, "suzuka", nil)
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

	// Race lap 1 (lap 1 of a session with total_laps > 0) is never eligible, even when it is the
	// fastest; a time-trial lap 1 (total_laps 0) is. s3 is a race whose total arrives late, s4 a time
	// trial. Eligibility follows the session's CURRENT total at read time.
	for _, l := range []*Lap{
		refLap("s3", 1, 2000, 50*sec, true, "monza", ConfidenceConfident),
		refLap("s3", 2, 2100, 120*sec, true, "monza", ConfidenceConfident),
		refLap("s4", 1, 3000, 55*sec, true, "monza", ConfidenceConfident),
	} {
		if err := s.Save(l); err != nil {
			t.Fatal(err)
		}
	}
	laps, _ = s.TrackLaps("monza", 10)
	assertLapIDs(t, "monza, total not yet known", laps, "s3#1", "s4#1", "s3#2")

	// A pin made while the total was unknown (the P1 window, or before the rule existed).
	if err := s.PinReference("monza", "s3", 1); err != nil {
		t.Fatal(err)
	}
	setTotal("s3", 10) // the total arrives after lap 1 was stored
	laps, _ = s.TrackLaps("monza", 10)
	assertLapIDs(t, "monza excludes race lap 1", laps, "s4#1", "s3#2")
	if l, _, _ := s.Lap("s3", 1); l.TotalLaps != 10 || RefEligible(l, "monza") {
		t.Errorf("s3#1 TotalLaps=%d eligible=%v, want 10 / false", l.TotalLaps, RefEligible(l, "monza"))
	}
	if l, _, _ := s.Lap("s4", 1); l.TotalLaps != 0 || !RefEligible(l, "monza") {
		t.Errorf("time-trial lap 1 TotalLaps=%d eligible=%v, want 0 / true", l.TotalLaps, RefEligible(l, "monza"))
	}
	if by := s.BySession("s3"); by[0].TotalLaps != 10 {
		t.Errorf("BySession TotalLaps = %d, want 10", by[0].TotalLaps)
	}

	// Resolution finds the pinned race lap 1 ineligible: the pin is removed (observably, one INFO log)
	// and the fastest eligible lap is the reference.
	core, logs := observer.New(zap.InfoLevel)
	ref, ok, err = ResolveTrackReference(s, "monza", zap.New(core))
	if err != nil || !ok || ref.Source != RefSourceFastest || lapID(&ref.Lap) != "s4#1" {
		t.Fatalf("monza resolve = %v %v %v %v, want fastest s4#1", lapID(&ref.Lap), ref.Source, ok, err)
	}
	if _, pinned, _ := s.PinnedReference("monza"); pinned {
		t.Error("ineligible pin was not removed")
	}
	if logs.FilterMessage("reference pin removed: lap not eligible").Len() != 1 {
		t.Errorf("want one pin-removed INFO log, got %v", logs.All())
	}
	ResolveTrackReference(s, "monza", zap.New(core)) //nolint:errcheck // second resolve: nothing left to remove
	if logs.Len() != 1 {
		t.Errorf("pin-removed log repeated: %d entries", logs.Len())
	}

	// Compare-and-delete: a stale read of an ineligible pin must not delete a newer pin. Simulate the
	// race by re-pinning between the read (s3#1) and the conditional unpin.
	if err := s.PinReference("monza", "s3", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.PinReference("monza", "s4", 1); err != nil { // the concurrent PUT
		t.Fatal(err)
	}
	if removed, err := s.UnpinReferenceIf("monza", "s3", 1); err != nil || removed {
		t.Errorf("stale UnpinReferenceIf = %v %v, want not removed", removed, err)
	}
	if p, ok, _ := s.PinnedReference("monza"); !ok || lapID(p) != "s4#1" {
		t.Errorf("newer pin lost: %v %v", lapID(p), ok)
	}
	if removed, err := s.UnpinReferenceIf("monza", "s4", 1); err != nil || !removed {
		t.Errorf("matching UnpinReferenceIf = %v %v, want removed", removed, err)
	}

	// The total only ever changes to a new positive value (the Detector keeps the last positive one;
	// a 0 after the race is never written), so a changed total keeps lap 1 excluded.
	setTotal("s3", 12)
	laps, _ = s.TrackLaps("monza", 10)
	assertLapIDs(t, "monza after total change", laps, "s4#1", "s3#2")
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
	totals := map[string]int32{}
	s.SessionTotalLaps = func(id string) int32 { return totals[id] }
	testTrackRefContract(t, s, func(id string) { _ = s.DeleteSession(id) }, func(id string, n int32) { totals[id] = n })
}

// testClearProvisionalContract is the shared Mem/Postgres contract for ClearProvisionalLabels: it
// nulls the track fields of the session's provisional laps only; confident and unlabelled laps and
// every other session are untouched. The sessions "c1", "c2" and "c3" must exist (Postgres FK).
func testClearProvisionalContract(t *testing.T, s trackRefStore) {
	t.Helper()
	sec := time.Second
	for _, l := range []*Lap{
		refLap("c1", 1, 0, 90*sec, true, "suzuka", ConfidenceConfident),
		refLap("c1", 2, 100, 90*sec, true, "brands-hatch-indy", ConfidenceProvisional),
		refLap("c1", 3, 200, 90*sec, true, "", ""),
		refLap("c1", 4, 300, 90*sec, true, "interlagos", ConfidenceProvisional),
		refLap("c2", 1, 0, 90*sec, true, "interlagos", ConfidenceProvisional),
	} {
		if err := s.Save(l); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ClearProvisionalLabels(context.Background(), "c1"); err != nil {
		t.Fatal(err)
	}
	for _, w := range []struct {
		session string
		lap     int32
		slug    string
		conf    string
	}{
		{"c1", 1, "suzuka", ConfidenceConfident}, {"c1", 2, "", ""}, {"c1", 3, "", ""}, {"c1", 4, "", ""},
		{"c2", 1, "interlagos", ConfidenceProvisional},
	} {
		l, ok, err := s.Lap(w.session, w.lap)
		if err != nil || !ok {
			t.Fatalf("Lap(%s,%d) = %v %v", w.session, w.lap, ok, err)
		}
		if l.TrackSlug != w.slug || l.TrackConfidence != w.conf {
			t.Errorf("%s#%d = %q/%q, want %q/%q", w.session, w.lap, l.TrackSlug, l.TrackConfidence, w.slug, w.conf)
		}
	}
	if err := s.ClearProvisionalLabels(context.Background(), "no-such-session"); err != nil {
		t.Errorf("unknown session: %v", err)
	}

	// keep spans protect provisional laps that started inside [From, Until) (Until zero = open-ended).
	for _, l := range []*Lap{
		refLap("c3", 1, 0, 90*sec, true, "a", ConfidenceProvisional),
		refLap("c3", 2, 100, 90*sec, true, "b", ConfidenceProvisional),
		refLap("c3", 3, 200, 90*sec, true, "c", ConfidenceProvisional),
		refLap("c3", 4, 300, 90*sec, true, "d", ConfidenceProvisional),
	} {
		if err := s.Save(l); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ClearProvisionalLabels(context.Background(), "c3", TimeSpan{From: at(100), Until: at(200)}, TimeSpan{From: at(300)}); err != nil {
		t.Fatal(err)
	}
	for lap, want := range map[int32]string{1: "", 2: "b", 3: "", 4: "d"} {
		l, _, _ := s.Lap("c3", lap)
		wantConf := ConfidenceProvisional
		if want == "" {
			wantConf = ""
		}
		if l.TrackSlug != want || l.TrackConfidence != wantConf {
			t.Errorf("c3#%d = %q/%q, want %q/%q", lap, l.TrackSlug, l.TrackConfidence, want, wantConf)
		}
	}
}

func TestMemLapStore_ClearProvisionalLabels(t *testing.T) {
	testClearProvisionalContract(t, NewMemLapStore())
}

// testLabelSpanContract: LabelTrack with a closed span labels only the laps that started inside
// [From, Until); laps from Until onward stay provisional. The session "c4" must exist (Postgres FK).
func testLabelSpanContract(t *testing.T, s trackRefStore) {
	t.Helper()
	sec := time.Second
	for i := int32(1); i <= 4; i++ {
		if err := s.Save(refLap("c4", i, int(i-1)*100, 90*sec, true, "guess", ConfidenceProvisional)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LabelTrack(context.Background(), "c4", TimeSpan{From: at(100), Until: at(300)}, "suzuka"); err != nil {
		t.Fatal(err)
	}
	for lap, want := range map[int32][2]string{1: {"guess", ConfidenceProvisional}, 2: {"suzuka", ConfidenceConfident},
		3: {"suzuka", ConfidenceConfident}, 4: {"guess", ConfidenceProvisional}} {
		l, _, _ := s.Lap("c4", lap)
		if l.TrackSlug != want[0] || l.TrackConfidence != want[1] {
			t.Errorf("c4#%d = %q/%q, want %q/%q", lap, l.TrackSlug, l.TrackConfidence, want[0], want[1])
		}
	}
}

func TestMemLapStore_LabelTrackSpan(t *testing.T) { testLabelSpanContract(t, NewMemLapStore()) }

// testSetSessionTrackContract: SetSessionTrack overwrites every lap of the session (unlabelled,
// provisional AND confident: the user's pick wins over an earlier detector lock) and leaves other
// sessions alone. Sessions "u1" and "u2" must exist (Postgres FK).
func testSetSessionTrackContract(t *testing.T, s trackRefStore) {
	t.Helper()
	sec := time.Second
	for i, conf := range []string{"", ConfidenceProvisional, ConfidenceConfident} {
		slug := ""
		if conf != "" {
			slug = "guess"
		}
		if err := s.Save(refLap("u1", int32(i+1), i*100, 90*sec, true, slug, conf)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(refLap("u2", 1, 0, 90*sec, true, "other", ConfidenceConfident)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionTrack(context.Background(), "u1", "suzuka", ConfidenceConfident); err != nil {
		t.Fatal(err)
	}
	for lap := int32(1); lap <= 3; lap++ {
		l, _, _ := s.Lap("u1", lap)
		if l.TrackSlug != "suzuka" || l.TrackConfidence != ConfidenceConfident {
			t.Errorf("u1#%d = %q/%q, want suzuka/confident", lap, l.TrackSlug, l.TrackConfidence)
		}
	}
	if l, _, _ := s.Lap("u2", 1); l.TrackSlug != "other" || l.TrackConfidence != ConfidenceConfident {
		t.Errorf("u2#1 = %q/%q, want other/confident untouched", l.TrackSlug, l.TrackConfidence)
	}
	// Withdrawing the pick downgrades the labels to provisional (detection owns the session again).
	if err := s.SetSessionTrack(context.Background(), "u1", "suzuka", ConfidenceProvisional); err != nil {
		t.Fatal(err)
	}
	if l, _, _ := s.Lap("u1", 2); l.TrackSlug != "suzuka" || l.TrackConfidence != ConfidenceProvisional {
		t.Errorf("u1#2 after withdraw = %q/%q, want suzuka/provisional", l.TrackSlug, l.TrackConfidence)
	}
}

func TestMemLapStore_SetSessionTrack(t *testing.T) { testSetSessionTrackContract(t, NewMemLapStore()) }
