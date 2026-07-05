package live

import (
	"sync"

	"github.com/arush-sal/garage/backend/rtps/analysis"
)

// Enrichment is the correlated data that fills in AFTER the raw frame (progressive enrichment, D-B24).
// Every correlated field (the pointer fields below, excluding the plain SessionID metadata) is
// nullable: it stays nil until its correlation resolves, then rides along on subsequent frames. Phase A
// populates Analysis (per completed lap); CarName/Track/DeltaToBestNs are reserved for later milestones
// (RKB / distance-indexed reference lap).
//
// IMPORTANT: Callers must NEVER mutate the pointees of pointer fields (Analysis, CarName, Track,
// DeltaToBestNs) after passing an Enrichment to Set. Set publishes a shallow copy, so mutation after
// publish is a data race. If you must modify analysis or metadata, construct a new Enrichment.
type Enrichment struct {
	SessionID     string                    `json:"session_id,omitempty"` // plain metadata; not a correlated field
	Analysis      *analysis.SessionAnalysis `json:"analysis"`
	CarName       *string                   `json:"car_name"`
	Track         *string                   `json:"track"`
	DeltaToBestNs *int64                    `json:"delta_to_best_ns"`
}

// EnrichmentState is the concurrency-safe holder the egress path reads and the processing side writes.
type EnrichmentState struct {
	mu  sync.RWMutex
	cur Enrichment
}

// NewEnrichmentState returns an empty state (all correlated fields nil).
func NewEnrichmentState() *EnrichmentState { return &EnrichmentState{} }

// Set replaces the current enrichment snapshot (called off the egress path as correlation resolves).
// See Enrichment's doc comment on immutability constraints.
func (e *EnrichmentState) Set(en Enrichment) {
	e.mu.Lock()
	e.cur = en
	e.mu.Unlock()
}

// Get returns the current enrichment snapshot (read per emit; never blocks the egress path meaningfully).
func (e *EnrichmentState) Get() Enrichment {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cur
}
