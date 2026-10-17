// Package session is the backend's Session Management domain (GARAGE domain 2). It consumes the
// collector's canonical TelemetryFrame stream and groups it into sessions — one contiguous on-track
// stint each — so downstream Processing can segment laps and Analysis can pick a Personal Best.
//
// The package is pure detection + persistence logic: it imports the generated telemetry types but
// NOT gRPC. The gRPC server that feeds it lives in rtps/ingest. Design + rationale: see the vault
// notes "Session Management Design v1" and "Backend Decision Log" (D-B1..D-B8).
//
// Scope boundary (D-B7): this package owns session lifecycle — boundaries, metadata, and the
// quit/abandon outcome tag. It performs NO in-race/lap analysis (laps, sectors, driving belong to
// Processing/Analysis). The only lap fields it reads are the collector's passthrough counters, used
// solely to set the outcome tag.
package session

import (
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
)

// Status is the lifecycle/outcome of a session as decided by the Detector at close time.
type Status string

const (
	// StatusActive means the session is still open — frames are still arriving.
	StatusActive Status = "active"

	// StatusFinished means the session reached the event's lap target at close
	// (current_lap >= total_laps). Eligible for Personal-Best analysis.
	StatusFinished Status = "finished"

	// StatusAbandoned means the session closed before reaching the lap target — a quit or restart
	// (current_lap < total_laps). Excluded from Personal-Best analysis. This is the "race quit"
	// outcome the Detector tags per D-B7 (e.g. the 4-race capture's race 3 quit at lap 1 of 10).
	StatusAbandoned Status = "abandoned"
)

// Record is one detected on-track stint. It is the structured output of Session Management:
// metadata + counts + the outcome tag, persisted via a Store and handed to a Sink.
//
// Lap fields are snapshotted from on-track frames, not the frame at close: CurrentLap/BestLap/LastLap
// from the LAST timed-lap frame (current_lap >= 1) and TotalLaps from the last frame reporting a lap
// target (> 0). After a race GT7 sends menu frames (-1) and on-track results-screen frames with lap 0
// and/or total_laps 0 (observed in the captures); reading those would capture garbage.
type Record struct {
	ID        string    // detector-assigned unique id
	StartTime time.Time // recv_timestamp of the frame that opened the session
	EndTime   time.Time // recv_timestamp of the last in-race frame (not the post-close menu frame)

	CarID   int32  // GT7 car model id
	CarName string // human-readable car name, resolved by the collector at the frame seam
	// CarCategory is GT7's format-C car category code (e.g. "GR4"); "" for format-A telemetry.
	CarCategory string
	Source      telemetryv1.Source // LIVE vs REPLAY — replay sessions must not pollute PB data

	FrameCount     uint64 // in-race frames attributed to this session
	GapCount       uint64 // frames whose Gap flag was set (a packet was lost before them)
	FirstPackageID uint32 // GT7 package_id at open — session provenance / ordering
	LastPackageID  uint32 // GT7 package_id at the last in-race frame

	// Passthrough lap counters from the last in-race frame — used only for the outcome tag (D-B7).
	CurrentLap int32
	TotalLaps  int32
	BestLap    time.Duration // 0 when GT7 reported no best lap yet
	LastLap    time.Duration // 0 when no lap completed yet

	Status Status // active while open; finished/abandoned once closed

	// Track and Driver are intentionally omitted in Slice 1: track id is not present in GT7 telemetry
	// (D-B3, deferred/user-supplied later) and the MVP is single-user (a default local profile).
}
