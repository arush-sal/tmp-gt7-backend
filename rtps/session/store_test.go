package session

import (
	"sync"
	"testing"
	"time"
)

// rec is a tiny helper to build a Record with just the fields the store cares about
// (identity + a timestamp for ordering). Keeps each test case to one readable line.
func rec(id string, car int32, start time.Time) *Record {
	return &Record{ID: id, CarID: car, StartTime: start, Status: StatusActive}
}

// TestMemStore_SaveGet verifies the basic round-trip: a saved record is retrievable by ID, and a
// miss reports found=false rather than a zero-value record (so callers can tell "absent" from "empty").
func TestMemStore_SaveGet(t *testing.T) {
	s := NewMemStore()
	r := rec("s1", 1384, time.Unix(100, 0))
	if err := s.Save(r); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok := s.Get("s1")
	if !ok {
		t.Fatal("Get(s1): want found, got miss")
	}
	if got.CarID != 1384 {
		t.Errorf("Get(s1).CarID = %d, want 1384", got.CarID)
	}
	if _, ok := s.Get("nope"); ok {
		t.Error("Get(nope): want miss, got found")
	}
}

// TestMemStore_SaveUpsert verifies Save is an upsert keyed by ID: re-saving the same ID updates the
// record in place (the detector saves a session active, then re-saves it closed) and does NOT create
// a duplicate List entry.
func TestMemStore_SaveUpsert(t *testing.T) {
	s := NewMemStore()
	_ = s.Save(rec("s1", 1384, time.Unix(100, 0)))

	updated := rec("s1", 1384, time.Unix(100, 0))
	updated.Status = StatusFinished
	_ = s.Save(updated)

	got, _ := s.Get("s1")
	if got.Status != StatusFinished {
		t.Errorf("after upsert Status = %q, want %q", got.Status, StatusFinished)
	}
	if n := len(s.List()); n != 1 {
		t.Errorf("List len = %d after upsert, want 1 (no duplicate)", n)
	}
}

// TestMemStore_ListOrder verifies List returns records in insertion order — deterministic output for
// tests and any future "recent sessions" view. Upserting an existing ID must not reorder it.
func TestMemStore_ListOrder(t *testing.T) {
	s := NewMemStore()
	_ = s.Save(rec("s1", 1, time.Unix(100, 0)))
	_ = s.Save(rec("s2", 2, time.Unix(200, 0)))
	_ = s.Save(rec("s3", 3, time.Unix(300, 0)))
	_ = s.Save(rec("s1", 1, time.Unix(100, 0))) // re-save first; must stay first

	want := []string{"s1", "s2", "s3"}
	got := s.List()
	if len(got) != len(want) {
		t.Fatalf("List len = %d, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("List[%d].ID = %q, want %q", i, got[i].ID, id)
		}
	}
}

// TestMemStore_SaveSnapshots verifies Save copies the record: mutating the caller's Record after
// Save must not change what the store returns. This decouples the store from the Detector, which
// keeps mutating its live record in place between the active Save and the final Save.
func TestMemStore_SaveSnapshots(t *testing.T) {
	s := NewMemStore()
	r := rec("s1", 1384, time.Unix(100, 0))
	_ = s.Save(r)

	r.Status = StatusAbandoned // mutate the original AFTER saving
	r.FrameCount = 999

	got, _ := s.Get("s1")
	if got.Status != StatusActive || got.FrameCount != 0 {
		t.Errorf("stored record reflects post-Save mutation: status=%q frames=%d, want active/0",
			got.Status, got.FrameCount)
	}
}

// TestMemStore_ConcurrentSaves runs many Saves from multiple goroutines. The assertion is not the
// count alone but that the race detector (go test -race) sees no data race on the shared map/slice —
// this is why MemStore is mutex-guarded (two gRPC streams can share one store).
func TestMemStore_ConcurrentSaves(t *testing.T) {
	s := NewMemStore()
	const goroutines, per = 8, 50

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range per {
				id := string(rune('A'+g)) + "-" + time.Duration(i).String()
				_ = s.Save(rec(id, int32(g), time.Unix(int64(i), 0)))
			}
		}(g)
	}
	wg.Wait()

	if n := len(s.List()); n != goroutines*per {
		t.Errorf("List len = %d, want %d", n, goroutines*per)
	}
}
