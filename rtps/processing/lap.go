// Package processing is the backend's Processing domain (GARAGE domain 3). It turns a session's
// telemetry stream into the canonical lap model — the source of truth for Analysis and Coaching.
//
// Slice 1 covers lap segmentation + lap times only. Sectors, corners, distance-normalization, and
// derived metrics are later slices. The segmenter consumes the Session Management Sink stream, so the
// same path serves both live (current-lap) and post-lap (all-laps) analysis — "one core flow, two
// depths". Design + decisions: vault "Session Management Design v1" / "Backend Decision Log" (D-B11+).
package processing

import "time"

// Lap is one segmented lap of a session — the canonical unit Analysis compares for Personal-Best.
//
// LapNumber mirrors GT7's current_lap: 0 is the formation/out lap (not a timed racing lap), 1..N are
// the timed laps. Duration is GT7's authoritative last_lap for laps closed by a lap-counter
// increment; for a lap closed by the session ending mid-lap it falls back to EndTime-StartTime and is
// marked incomplete.
type Lap struct {
	SessionID  string
	LapNumber  int32
	StartTime  time.Time     // recv_timestamp of the lap's first frame
	EndTime    time.Time     // recv_timestamp of the lap's last frame
	Duration   time.Duration // last_lap when complete; else EndTime-StartTime (fallback)
	FrameCount uint64
	GapCount   uint64 // frames with the gap flag — a gapped lap trace is unreliable

	Complete bool // a full lap closed by a lap-counter increment (false: cut by the session end, or a first lap the session joined mid-way)
	Timed    bool // a timed racing lap (LapNumber > 0); excludes the formation/out lap
	Valid    bool // usable for Personal-Best: Timed && Complete && GapCount == 0
	Fallback bool // Duration came from the wall-clock span (GT7 last_lap was unavailable), not an authoritative lap time

	// Fingerprint identifies this completed timed lap across sessions (session.LapFingerprint of the
	// rollover frame). Empty for the formation lap, partial laps and pre-migration rows. Unique across
	// stored laps: a replayed copy of a stored lap is how a duplicate session is recognised.
	Fingerprint string

	// TrackSlug and TrackConfidence are the track the live detector had for this lap ("" = none): set
	// when the lap completes (ConfidenceProvisional or ConfidenceConfident) and upgraded to confident
	// when the detector locks during the same detection stint (TrackRefStore.LabelTrack). The track
	// belongs to the lap, not the session: one session can drive several tracks.
	TrackSlug       string
	TrackConfidence string
}
