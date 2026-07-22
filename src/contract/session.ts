// src/contract/session.ts
// Wire shapes of the backend session/lap endpoints. snake_case = the wire.
// Verified against ../backend/rtps/api/laps.go + reference.go + rtps/live/payload.go (2026-07-22,
// backend commits 4c598b8 + a71a2be). Only the fields the frontend reads are listed.

export type SectorSplitDTO = {
  index: number;
  label: string;
  duration_ns: number;
  delta_to_best_ns: number | null; // null when the split has no trustworthy best pool
  min_speed: number;
};

export type LapSectorsDTO = {
  reliable: boolean;
  reason: string | null; // machine reason when !reliable: track-not-detected | no-centerline | no-sector-labels | trace-span-mismatch
  splits: SectorSplitDTO[];
};

// GET /v1/sessions/{id}/laps -> LapDTO[] (every lap, formation/partial/gapped included).
export type LapDTO = {
  lap_number: number;
  duration_ns: number;
  complete: boolean;
  valid: boolean;
  timed: boolean;
  delta_to_pb_ns: number | null; // vs stored GT7 best; null when no PB
  sectors: LapSectorsDTO;
};

// GET /v1/sessions/{id}/laps/{n}/trace?decimate=N -> TraceSampleDTO[]. t_ns = ns since lap t0.
// Phase 2 added fuel/tyre/clutch/boost/delta_to_best_ns. NOTE: no surface_type (kerb/grass ticks
// are unavailable for backend-served laps — cosmetic).
export type TraceSampleDTO = {
  seq: number;
  t_ns: number;
  distance: number;
  pos_x: number;
  pos_z: number;
  throttle: number;
  brake: number;
  speed: number;
  gear: number;
  rpm: number;
  fuel: number;
  tyre_temp_fl: number;
  tyre_temp_fr: number;
  tyre_temp_rl: number;
  tyre_temp_rr: number;
  clutch: number;
  boost: number;
  delta_to_best_ns: number | null;
};

// GET /v1/sessions/{id}/reference?channels=<csv>&points=N -> median across racing laps on a
// distance-fraction grid.
export type ReferenceDTO = {
  fractions: number[];            // grid points in [0,1], length = points (default 1000)
  series: Record<string, number[]>; // channel -> median value per grid point
  lap_count: number;
};
