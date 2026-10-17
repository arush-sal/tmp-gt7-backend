package processing

import (
	"errors"
	"slices"
	"sync"
)

// LapStore persists laps. Slice 1 ships only the in-memory MemLapStore; the interface exists so a
// Postgres laps table (FK to sessions) can replace it later without touching the segmenter — the same
// seam pattern as the session Store.
type LapStore interface {
	// Save stores one lap. The segmenter emits each (session, lap) exactly once, so this appends.
	Save(lap *Lap) error
	// BySession returns a session's laps in lap order (the primary query for Analysis/PB).
	BySession(sessionID string) []*Lap
	// List returns all laps, grouped by session in session-insertion order.
	List() []*Lap
	// SessionByFingerprint returns the session owning a lap with this fingerprint (never matches "").
	// A lookup failure is returned as err, never disguised as not-found.
	SessionByFingerprint(fp string) (sessionID string, found bool, err error)
	// DeleteSession removes all of a session's laps (rollback of a dropped duplicate session).
	DeleteSession(sessionID string) error
}

// ErrDuplicateFingerprint is returned (wrapped) by LapStore.Save for a fingerprint already stored under
// another lap: the laps.fingerprint unique index, mirrored by MemLapStore.
var ErrDuplicateFingerprint = errors.New("processing: lap fingerprint already stored")

// MemLapStore is a goroutine-safe in-memory LapStore. Like the session store, it is mutex-guarded
// because one segmenter (hence one store) may serve concurrent gRPC streams.
type MemLapStore struct {
	mu        sync.RWMutex
	bySession map[string][]*Lap
	order     []string          // session ids in first-seen order, for deterministic List
	byFP      map[string]string // lap fingerprint -> owning session id
}

// NewMemLapStore returns an empty in-memory lap store.
func NewMemLapStore() *MemLapStore {
	return &MemLapStore{bySession: make(map[string][]*Lap), byFP: make(map[string]string)}
}

// Save appends a snapshot of lap under its session. Lap has no pointer fields, so a shallow copy is a
// full snapshot — the store is decoupled from any mutation by the caller.
func (m *MemLapStore) Save(lap *Lap) error {
	snapshot := *lap
	m.mu.Lock()
	defer m.mu.Unlock()
	if lap.Fingerprint != "" {
		if _, dup := m.byFP[lap.Fingerprint]; dup {
			return ErrDuplicateFingerprint
		}
		m.byFP[lap.Fingerprint] = lap.SessionID
	}
	if _, ok := m.bySession[lap.SessionID]; !ok {
		m.order = append(m.order, lap.SessionID)
	}
	m.bySession[lap.SessionID] = append(m.bySession[lap.SessionID], &snapshot)
	return nil
}

// BySession returns a freshly-allocated slice of the session's laps (nil if none).
func (m *MemLapStore) BySession(sessionID string) []*Lap {
	m.mu.RLock()
	defer m.mu.RUnlock()
	laps := m.bySession[sessionID]
	if laps == nil {
		return nil // unknown session — distinct from a session with zero laps
	}
	return copyLaps(laps)
}

// List returns all laps, grouped by session in first-seen order.
func (m *MemLapStore) List() []*Lap {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Lap
	for _, id := range m.order {
		out = append(out, copyLaps(m.bySession[id])...)
	}
	return out
}

// copyLaps returns fresh *Lap snapshots so callers can't mutate the store's copies through returned
// pointers (Lap has no pointer fields, so a value copy is a full snapshot).
func copyLaps(laps []*Lap) []*Lap {
	out := make([]*Lap, len(laps))
	for i, l := range laps {
		c := *l
		out[i] = &c
	}
	return out
}

// SessionByFingerprint returns the session owning fp (never matches the empty fingerprint).
func (m *MemLapStore) SessionByFingerprint(fp string) (string, bool, error) {
	if fp == "" {
		return "", false, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byFP[fp]
	return id, ok, nil
}

// DeleteSession removes a session's laps and their fingerprints (no-op for an unknown session).
func (m *MemLapStore) DeleteSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.bySession[sessionID] {
		delete(m.byFP, l.Fingerprint)
	}
	delete(m.bySession, sessionID)
	m.order = slices.DeleteFunc(m.order, func(id string) bool { return id == sessionID })
	return nil
}
