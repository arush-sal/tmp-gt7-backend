package live

import (
	"sync"

	"github.com/arush-sal/garage/backend/rtps/analysis"
)

// Enrichment is the correlated data that fills in AFTER the raw frame (progressive enrichment, D-B24).
// Every correlated field (the pointer fields below, excluding the plain SessionID metadata) is
// nullable: it stays nil until its correlation resolves, then rides along on subsequent frames. Phase A
// populates Analysis (per completed lap); Track/TrackConfidence are populated by the TrackDetector
// (Phase 2); CarName/DeltaToBestNs are reserved for later milestones (RKB / distance-indexed reference
// lap).
//
// IMPORTANT: Callers must NEVER mutate the pointees of pointer fields (Analysis, CarName, Track,
// TrackConfidence, DeltaToBestNs) after passing an Enrichment to Set. Set publishes a shallow copy, so
// mutation after publish is a data race. If you must modify analysis or metadata, construct a new
// Enrichment.
type Enrichment struct {
	SessionID       string                    `json:"session_id"` // plain metadata; not a correlated field. Always serialized (never omitempty): the frontend keys every live frame on it.
	Analysis        *analysis.SessionAnalysis `json:"analysis"`
	CarName         *string                   `json:"car_name"`
	Track           *string                   `json:"track"`
	TrackConfidence *string                   `json:"track_confidence"` // "provisional" (may still switch) or "confident" (locked); nil until a first guess
	DeltaToBestNs   *int64                    `json:"delta_to_best_ns"`
	// DuplicateOf is the id of the stored ORIGINAL session while the live session is a dropped
	// duplicate of it (an in-game replay of laps already stored); SessionID then equals it too, so the
	// frontend links to the original. Omitted (absent from JSON) when the live session is not a duplicate.
	DuplicateOf string `json:"duplicate_of,omitempty"`
	// Recording is the in-process stream's Record switch: true by default and again at every new
	// session, false after the user turned recording off for the current session. Always serialized.
	// The API overlays it from the RecordingControl when serving (EnrichmentState does not track it).
	Recording bool `json:"recording"`
}

// EnrichmentState is the concurrency-safe holder the egress path reads and the processing side writes.
//
// Multiple independent writers publish into the same snapshot (the analysis enrich-sink writes
// Analysis; the TrackDetector writes Track/TrackConfidence). A plain replace-the-whole-snapshot Set
// would let one writer clobber the other's already-resolved field, so writers should prefer the
// field-scoped setters below (SetAnalysis, SetTrack, ClearTrack), which lock, mutate only their own
// field(s), and preserve the rest of the current snapshot.
type EnrichmentState struct {
	mu  sync.RWMutex
	cur Enrichment
}

// NewEnrichmentState returns an empty state (all correlated fields nil).
func NewEnrichmentState() *EnrichmentState { return &EnrichmentState{} }

// Set replaces the current enrichment snapshot (called off the egress path as correlation resolves).
// See Enrichment's doc comment on immutability constraints. Prefer the field-scoped setters
// (SetAnalysis, SetTrack, ClearTrack) when another writer may hold fields you must not clobber.
func (e *EnrichmentState) Set(en Enrichment) {
	e.mu.Lock()
	e.cur = en
	e.mu.Unlock()
}

// SetAnalysis updates SessionID + Analysis under lock, preserving Track/TrackConfidence/CarName/
// DeltaToBestNs already published (e.g. by the TrackDetector) so this writer never clobbers them.
func (e *EnrichmentState) SetAnalysis(sessionID string, a *analysis.SessionAnalysis) {
	e.mu.Lock()
	e.cur.SessionID = sessionID
	e.cur.Analysis = a
	e.mu.Unlock()
}

// SetTrack updates SessionID + Track + TrackConfidence under lock, preserving Analysis/CarName/
// DeltaToBestNs already published (e.g. by the analysis enrich-sink) so this writer never clobbers them.
func (e *EnrichmentState) SetTrack(sessionID string, slug *string, confidence *string) {
	e.mu.Lock()
	e.cur.SessionID = sessionID
	e.cur.Track = slug
	e.cur.TrackConfidence = confidence
	e.mu.Unlock()
}

// ClearTrack nils Track + TrackConfidence under lock (e.g. on a new session where track correlation
// has not yet resolved), preserving Analysis/CarName/DeltaToBestNs.
func (e *EnrichmentState) ClearTrack(sessionID string) {
	e.mu.Lock()
	e.cur.SessionID = sessionID
	e.cur.Track = nil
	e.cur.TrackConfidence = nil
	e.mu.Unlock()
}

// SetDuplicate marks the live session as a dropped duplicate of originalID: SessionID and DuplicateOf
// both become originalID, preserving every other field. The next Set (a new session) clears it.
func (e *EnrichmentState) SetDuplicate(originalID string) {
	e.mu.Lock()
	e.cur.SessionID = originalID
	e.cur.DuplicateOf = originalID
	e.mu.Unlock()
}

// ClearSession removes the live link to a session that was discarded without a duplicate original (no
// completed lap, or recording switched off): SessionID becomes "" (the "no session" value, as when
// idle) and its Analysis and DuplicateOf are cleared, so clients never fetch a deleted session. The
// Track is kept: it describes where the car is, not the stored session. The next session start
// publishes the new id as usual.
func (e *EnrichmentState) ClearSession() {
	e.mu.Lock()
	e.cur.SessionID = ""
	e.cur.Analysis = nil
	e.cur.DuplicateOf = ""
	e.mu.Unlock()
}

// Get returns the current enrichment snapshot (read per emit; never blocks the egress path meaningfully).
func (e *EnrichmentState) Get() Enrichment {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cur
}
