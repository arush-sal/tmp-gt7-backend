package processing

import "testing"

// TestMemLapStore covers save/query/order and the snapshot-on-save decoupling.
func TestMemLapStore(t *testing.T) {
	s := NewMemLapStore()
	_ = s.Save(&Lap{SessionID: "a", LapNumber: 0})
	_ = s.Save(&Lap{SessionID: "a", LapNumber: 1})
	_ = s.Save(&Lap{SessionID: "b", LapNumber: 0})

	if got := s.BySession("a"); len(got) != 2 || got[1].LapNumber != 1 {
		t.Errorf("BySession(a) = %+v, want 2 laps ending at lap 1", got)
	}
	if got := s.BySession("missing"); got != nil {
		t.Errorf("BySession(missing) = %v, want nil", got)
	}
	// List groups by session in first-seen order (a before b).
	all := s.List()
	if len(all) != 3 || all[0].SessionID != "a" || all[2].SessionID != "b" {
		t.Errorf("List = %+v, want a,a,b", all)
	}
}

// TestMemLapStore_SaveSnapshots verifies Save copies the lap (mutating the caller's lap after Save
// does not change the stored copy).
func TestMemLapStore_SaveSnapshots(t *testing.T) {
	s := NewMemLapStore()
	l := &Lap{SessionID: "a", LapNumber: 1, Valid: true}
	_ = s.Save(l)
	l.Valid = false
	l.LapNumber = 99
	if got := s.BySession("a"); !got[0].Valid || got[0].LapNumber != 1 {
		t.Errorf("stored lap reflects post-Save mutation: %+v", got[0])
	}
}

// TestMemLapStore_ReadReturnsCopy verifies mutating a lap returned by BySession/List does not change
// the store's copy (read-side snapshot).
func TestMemLapStore_ReadReturnsCopy(t *testing.T) {
	s := NewMemLapStore()
	_ = s.Save(&Lap{SessionID: "a", LapNumber: 1, Valid: true})

	got := s.BySession("a")
	got[0].Valid = false
	got[0].LapNumber = 42

	again := s.BySession("a")
	if !again[0].Valid || again[0].LapNumber != 1 {
		t.Errorf("mutating a returned lap changed the store: %+v", again[0])
	}
}
