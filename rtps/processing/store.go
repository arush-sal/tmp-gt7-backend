package processing

import (
	"errors"
	"slices"
	"sort"
	"sync"
	"time"
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
	pins      map[string]lapKey // track slug -> pinned reference lap

	// SessionTotalLaps reports a session's total_laps (0 = unknown or a time trial), read at every
	// lap read to fill Lap.TotalLaps: the memory counterpart of PostgresLapStore's sessions-row
	// subquery, so both stores judge race-lap-1 eligibility from the same session value. nil = 0.
	SessionTotalLaps func(sessionID string) int32
}

// lapKey identifies one stored lap.
type lapKey struct {
	session string
	lap     int32
}

// NewMemLapStore returns an empty in-memory lap store.
func NewMemLapStore() *MemLapStore {
	return &MemLapStore{bySession: make(map[string][]*Lap), byFP: make(map[string]string), pins: make(map[string]lapKey)}
}

// Save appends a snapshot of lap under its session. Lap has no pointer fields, so a shallow copy is a
// full snapshot — the store is decoupled from any mutation by the caller.
func (m *MemLapStore) Save(lap *Lap) error {
	snapshot := *lap
	snapshot.TotalLaps = 0 // never stored: read time fills it from the session (see SessionTotalLaps)
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
	return m.copyLaps(laps)
}

// List returns all laps, grouped by session in first-seen order.
func (m *MemLapStore) List() []*Lap {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Lap
	for _, id := range m.order {
		out = append(out, m.copyLaps(m.bySession[id])...)
	}
	return out
}

// copyLaps returns fresh *Lap snapshots so callers can't mutate the store's copies through returned
// pointers (Lap has no pointer fields, so a value copy is a full snapshot).
func (m *MemLapStore) copyLaps(laps []*Lap) []*Lap {
	out := make([]*Lap, len(laps))
	for i, l := range laps {
		out[i] = m.view(l)
	}
	return out
}

// view returns a snapshot of the stored lap with TotalLaps filled from its session.
func (m *MemLapStore) view(l *Lap) *Lap {
	c := *l
	if m.SessionTotalLaps != nil {
		c.TotalLaps = m.SessionTotalLaps(l.SessionID)
	}
	return &c
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

// DeleteSession removes a session's laps, their fingerprints and any pin on them (the Postgres FK
// cascade's equivalent). No-op for an unknown session.
func (m *MemLapStore) DeleteSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.bySession[sessionID] {
		delete(m.byFP, l.Fingerprint)
	}
	for slug, k := range m.pins {
		if k.session == sessionID {
			delete(m.pins, slug)
		}
	}
	delete(m.bySession, sessionID)
	m.order = slices.DeleteFunc(m.order, func(id string) bool { return id == sessionID })
	return nil
}

// find returns the stored lap (not a copy) or nil. Caller holds m.mu.
func (m *MemLapStore) find(sessionID string, lapNumber int32) *Lap {
	for _, l := range m.bySession[sessionID] {
		if l.LapNumber == lapNumber {
			return l
		}
	}
	return nil
}

// LabelTrack implements TrackRefStore.
func (m *MemLapStore) LabelTrack(sessionID string, since time.Time, slug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.bySession[sessionID] {
		if !l.StartTime.Before(since) && (l.TrackConfidence == "" || l.TrackConfidence == ConfidenceProvisional) {
			l.TrackSlug, l.TrackConfidence = slug, ConfidenceConfident
		}
	}
	return nil
}

// Lap implements TrackRefStore.
func (m *MemLapStore) Lap(sessionID string, lapNumber int32) (*Lap, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if l := m.find(sessionID, lapNumber); l != nil {
		return m.view(l), true, nil
	}
	return nil, false, nil
}

// TrackLaps implements TrackRefStore: eligible laps by duration, ties by start time.
func (m *MemLapStore) TrackLaps(slug string, limit int) ([]*Lap, error) {
	m.mu.RLock()
	var out []*Lap
	for _, laps := range m.bySession {
		for _, l := range laps {
			if c := m.view(l); RefEligible(c, slug) {
				out = append(out, c)
			}
		}
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Duration != out[j].Duration {
			return out[i].Duration < out[j].Duration
		}
		return out[i].StartTime.Before(out[j].StartTime)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// PinnedReference implements TrackRefStore.
func (m *MemLapStore) PinnedReference(slug string) (*Lap, bool, error) {
	m.mu.RLock()
	k, ok := m.pins[slug]
	m.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	return m.Lap(k.session, k.lap)
}

// PinReference implements TrackRefStore.
func (m *MemLapStore) PinReference(slug, sessionID string, lapNumber int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.find(sessionID, lapNumber) == nil {
		return ErrLapNotFound
	}
	m.pins[slug] = lapKey{sessionID, lapNumber}
	return nil
}

// UnpinReference implements TrackRefStore.
func (m *MemLapStore) UnpinReference(slug string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.pins[slug]
	delete(m.pins, slug)
	return ok, nil
}

// UnpinReferenceIf implements TrackRefStore.
func (m *MemLapStore) UnpinReferenceIf(slug, sessionID string, lapNumber int32) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if k, ok := m.pins[slug]; !ok || k != (lapKey{sessionID, lapNumber}) {
		return false, nil
	}
	delete(m.pins, slug)
	return true, nil
}
