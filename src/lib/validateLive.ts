// src/lib/validateLive.ts
import type { LiveSnapshot, Frame, Derived, Enrichment } from "../contract/live";
import { parseDuration } from "./format";

function obj(v: unknown, path: string): Record<string, unknown> {
  if (typeof v !== "object" || v === null) throw new Error(`${path}: expected a non-null object`);
  return v as Record<string, unknown>;
}
function num(v: unknown, path: string): number {
  if (typeof v !== "number" || !Number.isFinite(v)) throw new Error(`${path}: expected a number`);
  return v;
}
function safeIntNs(v: unknown, path: string): number {
  const n = num(v, path);
  if (!Number.isSafeInteger(n)) throw new Error(`${path}: ns exceeds safe integer range`);
  return n;
}
function bool(v: unknown, path: string): boolean {
  if (typeof v !== "boolean") throw new Error(`${path}: expected a boolean`);
  return v;
}
function durationOrNull(v: unknown, path: string): string | null {
  if (v === null) return null;
  if (typeof v !== "string") throw new Error(`${path}: expected a Duration string or null`);
  try {
    parseDuration(v); // throws on malformed / negative / unsafe-int
  } catch (e) {
    throw new Error(`${path}: ${e instanceof Error ? e.message : String(e)}`); // scope the error to the field
  }
  return v;
}

function validateFrame(raw: unknown): Frame {
  const f = obj(raw, "frame");
  const p = (k: string) => `frame.${k}`;
  return {
    car_speed: num(f.car_speed, p("car_speed")),
    current_gear: num(f.current_gear, p("current_gear")),
    suggested_gear: num(f.suggested_gear, p("suggested_gear")),
    rpm: num(f.rpm, p("rpm")),
    throttle: num(f.throttle, p("throttle")),
    brake: num(f.brake, p("brake")),
    clutch: num(f.clutch, p("clutch")),
    boost: num(f.boost, p("boost")),
    current_fuel: num(f.current_fuel, p("current_fuel")),
    fuel_capacity: num(f.fuel_capacity, p("fuel_capacity")),
    tyre_temp_fl: num(f.tyre_temp_fl, p("tyre_temp_fl")),
    tyre_temp_fr: num(f.tyre_temp_fr, p("tyre_temp_fr")),
    tyre_temp_rl: num(f.tyre_temp_rl, p("tyre_temp_rl")),
    tyre_temp_rr: num(f.tyre_temp_rr, p("tyre_temp_rr")),
    current_lap: num(f.current_lap, p("current_lap")),
    total_laps: num(f.total_laps, p("total_laps")),
    current_position: num(f.current_position, p("current_position")),
    total_positions: num(f.total_positions, p("total_positions")),
    best_lap: durationOrNull(f.best_lap, p("best_lap")),
    last_lap: durationOrNull(f.last_lap, p("last_lap")),
    in_race: bool(f.in_race, p("in_race")),
    is_paused: bool(f.is_paused, p("is_paused")),
    ...formatCFields(f),
  };
}

// GT7 format-C frame fields (frontend-handoff.md). Optional: absent on format-A / older sources.
// Read leniently — a field is included only when present with the right type, and a wrong-typed
// value is dropped (not thrown), matching the "treat unknown fields leniently" contract.
const FORMAT_C_NUM = [
  "current_lap_time_ms", "wheel_steering_angle_front_left", "wheel_steering_angle_front_right",
  "wheel_base", "throttle_filtered", "wheel_rotation", "steering_angular_velocity",
  "sway", "heave", "surge", "brake_filtered",
  "torque_vector_fl", "torque_vector_fr", "torque_vector_rl", "torque_vector_rr",
  "energy_recovery", "unknown_u8_13e", "unknown_u8_13f", "unknown_f32_154",
  "position_x", "position_y", "position_z",
] as const;
const FORMAT_C_STR = ["surface_type", "car_category"] as const;

function formatCFields(f: Record<string, unknown>): Partial<Frame> {
  const out: Record<string, number | string> = {};
  for (const k of FORMAT_C_NUM) {
    const v = f[k];
    if (typeof v === "number" && Number.isFinite(v)) out[k] = v;
  }
  for (const k of FORMAT_C_STR) {
    const v = f[k];
    if (typeof v === "string") out[k] = v;
  }
  return out as Partial<Frame>;
}

function validateDerived(raw: unknown): Derived {
  const d = obj(raw, "derived");
  return {
    current_lap_time_ns: safeIntNs(d.current_lap_time_ns, "derived.current_lap_time_ns"),
    fuel_pct: num(d.fuel_pct, "derived.fuel_pct"),
    source: typeof d.source === "string" ? d.source : (() => { throw new Error("derived.source: expected a string"); })(),
  };
}

function validateEnrichment(raw: unknown): Enrichment {
  const e = obj(raw, "enrichment");
  for (const k of ["analysis", "car_name", "track", "delta_to_best_ns"]) {
    if (!(k in e)) throw new Error(`enrichment.${k}: required key missing`);
  }
  if ("session_id" in e && typeof e.session_id !== "string") {
    throw new Error("enrichment.session_id: expected a string when present");
  }
  // track_confidence: optional string | null; read leniently (drop wrong-typed rather than throw).
  const trackConfidence =
    typeof e.track_confidence === "string" || e.track_confidence === null
      ? (e.track_confidence as string | null)
      : undefined;
  if (e.car_name !== null && typeof e.car_name !== "string") throw new Error("enrichment.car_name: expected string or null");
  if (e.track !== null && typeof e.track !== "string") throw new Error("enrichment.track: expected string or null");
  const delta = e.delta_to_best_ns === null ? null : safeIntNs(e.delta_to_best_ns, "enrichment.delta_to_best_ns");
  // analysis: null or a plain object (not an array); not deep-validated this slice.
  if (e.analysis !== null && (typeof e.analysis !== "object" || Array.isArray(e.analysis))) throw new Error("enrichment.analysis: expected object or null");
  return {
    ...(typeof e.session_id === "string" ? { session_id: e.session_id } : {}),
    analysis: (e.analysis as object | null) ?? null,
    car_name: (e.car_name as string | null),
    track: (e.track as string | null),
    delta_to_best_ns: delta,
    ...(trackConfidence !== undefined ? { track_confidence: trackConfidence } : {}),
  };
}

export function validateLive(raw: unknown): LiveSnapshot {
  const s = obj(raw, "snapshot");
  if (typeof s.served_at !== "string") throw new Error("snapshot.served_at: expected a string");
  if (s.status !== "live" && s.status !== "idle") throw new Error("snapshot.status: expected 'live' or 'idle'");
  const enrichment = validateEnrichment(s.enrichment);

  if (s.status === "idle") {
    if (s.frame !== null) throw new Error("snapshot.frame: must be null when status is idle");
    if (s.derived !== null) throw new Error("snapshot.derived: must be null when status is idle");
    return { served_at: s.served_at, status: "idle", frame: null, derived: null, enrichment };
  }
  // live
  if (s.frame === null) throw new Error("snapshot.frame: must be present when status is live");
  if (s.derived === null) throw new Error("snapshot.derived: must be present when status is live");
  return {
    served_at: s.served_at,
    status: "live",
    frame: validateFrame(s.frame),
    derived: validateDerived(s.derived),
    enrichment,
  };
}
