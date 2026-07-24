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
// Phase 2 added fuel/tyre/clutch/boost/delta_to_best_ns; D-B30 added surface_type.
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
  surface_type: string; // per-wheel GT7 surface code FL,FR,RL,RR (T tarmac C kerb D dirt G grass S sand s snow); "" = no data
};

// GET /v1/sessions/{id}/coaching -> analysis (predicted-best etc.) + a pending coaching layer.
// 404 if the session is unknown, 409 while it's still active (use /v1/live during a race).
export type CoachingReport = {
  analysis: { predicted_best_ns?: number } | null;
  coaching: { status: string; note: string };
};

// GET /v1/sessions/{id}/median?channels=<csv>&points=N -> per-channel median across racing laps on
// a distance-fraction grid. (This is the shape the old /reference served pre-D-B28.)
export type MedianDTO = {
  fractions: number[];            // grid points in [0,1], length = points (default 1000)
  series: Record<string, number[]>; // channel -> median value per grid point (speed/throttle/brake/rpm/boost/gear)
  lap_count: number;
};

// GET /v1/sessions/{id}/reference?decimate=N -> the predicted (ideal) lap: fastest sector segments
// spliced into one synthetic lap's full telemetry trace. Verified against ../backend/rtps/api/reference.go.
export type PredictedSampleDTO = {
  fraction: number;   // 0..1 track position (shared axis with trace + median)
  t_ns: number;       // elapsed ns in the synthetic lap
  distance: number;   // re-accumulated spliced metres
  pos_x: number;
  pos_z: number;
  speed: number;
  throttle: number;
  brake: number;
  rpm: number;
  gear: number;
  fuel: number;
  tyre_temp_fl: number;
  tyre_temp_fr: number;
  tyre_temp_rl: number;
  tyre_temp_rr: number;
  clutch: number;
  boost: number;
  delta_to_best_ns: number | null;
};

export type ReferenceDTO = {
  reliable: boolean;
  reason: string | null;           // machine reason when !reliable (splice used no RKB geometry)
  predicted_best_ns: number;
  sectors: { index: number; label: string; owner_lap: number; duration_ns: number }[];
  samples: PredictedSampleDTO[];
};

// GET /v1/sessions/{id}/laps/{n}/peaks?reference=<predicted|lapNumber> -> significant speed peaks
// (local maxima) and valleys (local minima), each annotated with a time delta to a reference.
export type ExtremumKind = "peak" | "valley";

export type ExtremumDTO = {
  index: number;
  kind: ExtremumKind;
  fraction: number;        // 0..1 track position (shared axis with trace + track map)
  distance: number;        // cumulative in-lap metres
  speed: number;           // km/h, raw
  delta_ns: number | null; // ns vs reference (+ == this lap behind); null when reference unusable
};

export type PeaksReferenceDTO = {
  kind: "predicted" | "lap";
  lap_number: number | null; // set only when kind === "lap"
};

export type PeaksDTO = {
  reliable: boolean;
  reason: string | null;           // trace-span-mismatch, etc. — null when reliable
  reference: PeaksReferenceDTO;
  reference_reliable: boolean;
  reference_reason: string | null; // null when reference_reliable
  points: ExtremumDTO[];
};
