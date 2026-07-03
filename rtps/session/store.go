package session

import "sync"

// Store persists Records. Slice 1 ships only the in-memory MemStore; the interface
// exists now so a PostgreSQL/TimescaleDB implementation can replace it later without touching the
// Detector (D-B5). Save is an upsert keyed by Record.ID.
type Store interface {
	// Save inserts or updates the record by its ID. The Detector calls it when a session opens
	// (active) and again when it closes (finished/abandoned).
	Save(rec *Record) error
	// Get returns the record for id. found is false when no such record exists — distinct from a
	// zero-value record, so callers can tell "absent" from "present but empty".
	Get(id string) (rec *Record, found bool)
	// List returns all records in insertion order (first-saved first). Order is stable across
	// upserts of an existing id.
	List() []*Record
}

// MemStore is a goroutine-safe in-memory Store.
//
// It is mutex-guarded because a single store may be shared across concurrent gRPC ingest streams
// (each stream runs the Detector on its own goroutine). The MVP expects one collector, but the store
// stays safe regardless rather than depending on that assumption.
type MemStore struct {
	mu    sync.RWMutex
	byID  map[string]*Record
	order []string // ids in first-insertion order, so List is deterministic
}

// NewMemStore returns an empty, ready-to-use in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{byID: make(map[string]*Record)}
}

// Save upserts a snapshot of rec by rec.ID. It stores a COPY, not the caller's pointer: the Detector
// keeps mutating its live Record in place between the active Save and the final Save, and the store
// must not expose those mid-flight mutations to a concurrent reader. Record has no pointer fields, so
// a shallow copy is a full snapshot. A new id is appended to the insertion order; re-saving an
// existing id replaces its snapshot and leaves its position in List unchanged.
func (m *MemStore) Save(rec *Record) error {
	snapshot := *rec
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byID[rec.ID]; !exists {
		m.order = append(m.order, rec.ID)
	}
	m.byID[rec.ID] = &snapshot
	return nil
}

// Get returns the stored record for id and whether it was found.
func (m *MemStore) Get(id string) (*Record, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.byID[id]
	return rec, ok
}

// List returns the records in insertion order. The returned slice is freshly allocated (safe for the
// caller to keep), though the record pointers are shared with the store.
func (m *MemStore) List() []*Record {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Record, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.byID[id])
	}
	return out
}
