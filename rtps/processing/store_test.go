package processing

import (
	"errors"
	"testing"
)

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

// TestMemLapStore_Fingerprint: fingerprints are unique across laps (empty = none, never conflicts),
// SessionByFingerprint finds the owner, and DeleteSession removes a session's laps and fingerprints.
func TestMemLapStore_Fingerprint(t *testing.T) {
	s := NewMemLapStore()
	if err := s.Save(&Lap{SessionID: "a", LapNumber: 0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(&Lap{SessionID: "b", LapNumber: 0}); err != nil {
		t.Fatalf("two empty fingerprints must not conflict: %v", err)
	}
	if err := s.Save(&Lap{SessionID: "a", LapNumber: 1, Fingerprint: "fp1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(&Lap{SessionID: "b", LapNumber: 1, Fingerprint: "fp1"}); !errors.Is(err, ErrDuplicateFingerprint) {
		t.Errorf("duplicate fingerprint save = %v, want ErrDuplicateFingerprint", err)
	}
	if id, ok, err := s.SessionByFingerprint("fp1"); err != nil || !ok || id != "a" {
		t.Errorf("SessionByFingerprint(fp1) = %q,%v want a,true", id, ok)
	}
	if _, ok, _ := s.SessionByFingerprint(""); ok {
		t.Error("empty fingerprint must never match")
	}
	if err := s.DeleteSession("a"); err != nil {
		t.Fatal(err)
	}
	if s.BySession("a") != nil {
		t.Error("laps of deleted session still present")
	}
	if _, ok, _ := s.SessionByFingerprint("fp1"); ok {
		t.Error("deleted session's fingerprint still indexed")
	}
	if all := s.List(); len(all) != 1 || all[0].SessionID != "b" {
		t.Errorf("List after delete = %+v, want only b", all)
	}
}

// TestMemTraceStore_DeleteSession removes only that session's samples.
func TestMemTraceStore_DeleteSession(t *testing.T) {
	s := NewMemTraceStore()
	_ = s.SaveBatch([]Sample{{SessionID: "a", LapNumber: 1}, {SessionID: "b", LapNumber: 1}})
	if err := s.DeleteSession("a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ByLap("a", 1); len(got) != 0 {
		t.Errorf("a still has %d samples", len(got))
	}
	if got, _ := s.ByLap("b", 1); len(got) != 1 {
		t.Errorf("b has %d samples, want 1", len(got))
	}
}
