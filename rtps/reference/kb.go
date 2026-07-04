// Package reference is the seam to the Reference Knowledge Base (RKB): track identity + sector
// definitions. The backend depends on the KB interface; the real RKB is a separate, config-provided
// database the user populates (D-B16/D-B17). This package ships a static in-memory KB for tests and
// mock data so downstream (sector segmentation, Analysis) is unblocked before the RKB exists.
package reference

import "context"

// Track is a resolved track/layout from the RKB.
type Track struct {
	ID      string
	Name    string
	LengthM float64
}

// Sector is one sector of a track, as a normalized lap-distance range [Start, End] in [0,1]
// (coordinates-first, not corner indices — D-B16). Sectors of a track are contiguous and ordered.
type Sector struct {
	Index int
	Start float64
	End   float64
}

// KB is the read-only Reference Knowledge Base contract. The real implementation reads the RKB
// database; a stub/static impl is used until it's wired.
type KB interface {
	// TrackForSession resolves the track a session was run on. found is false when the RKB can't
	// identify it (telemetry has no track id; the RKB owns detection).
	TrackForSession(ctx context.Context, sessionID string) (track Track, found bool, err error)
	// Sectors returns a track's sector boundaries, ordered by Index. Empty (not an error) if the RKB
	// has no sector definition for the track.
	Sectors(ctx context.Context, trackID string) ([]Sector, error)
}

// StaticKB is an in-memory KB for tests and mock data. Unknown session/track resolve to not-found /
// empty. It is read-only after population (populate before use); safe for concurrent reads.
type StaticKB struct {
	trackBySession map[string]Track
	sectorsByTrack map[string][]Sector
}

// NewStaticKB returns an empty static KB.
func NewStaticKB() *StaticKB {
	return &StaticKB{trackBySession: map[string]Track{}, sectorsByTrack: map[string][]Sector{}}
}

// Add registers a track for a session plus that track's sectors (mock-data helper).
func (k *StaticKB) Add(sessionID string, t Track, sectors []Sector) {
	k.trackBySession[sessionID] = t
	k.sectorsByTrack[t.ID] = sectors
}

// TrackForSession implements KB.
func (k *StaticKB) TrackForSession(_ context.Context, sessionID string) (Track, bool, error) {
	t, ok := k.trackBySession[sessionID]
	return t, ok, nil
}

// Sectors implements KB.
func (k *StaticKB) Sectors(_ context.Context, trackID string) ([]Sector, error) {
	return k.sectorsByTrack[trackID], nil
}

// MockTrack is representative mock data (a 3-sector track) to unblock development before the RKB is
// available. Boundaries are illustrative normalized fractions, not real coordinates.
func MockTrack() (Track, []Sector) {
	return Track{ID: "mock-gp", Name: "Mock GP Circuit", LengthM: 4000},
		[]Sector{{Index: 0, Start: 0, End: 0.4}, {Index: 1, Start: 0.4, End: 0.75}, {Index: 2, Start: 0.75, End: 1}}
}
