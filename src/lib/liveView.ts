import type { LiveSnapshot } from "../contract/live";
import { nsToClock, nsToDeltaSeconds, parseDuration, PLACEHOLDER } from "./format";

export type TyreTemp = { value: string; tone: "hot" | "warm" | "cold" | "none" };

export type LiveView = {
  status: "live" | "idle";
  speed: string;
  gear: string;
  rpm: string;
  lap: string;
  position: string;
  fuelPct: string;
  currentLapTime: string;
  delta: string;
  bestLap: string;
  lastLap: string;
  tyreTemps: { fl: TyreTemp; fr: TyreTemp; rl: TyreTemp; rr: TyreTemp };
  deltaTone: "blue" | "red" | "none";
  fuelTone: "green" | "yellow" | "red" | "none";
};

// bands (see plan Global Constraints)
function tyreTone(c: number): "hot" | "warm" | "cold" {
  if (c > 85) return "hot";
  if (c < 55) return "cold";
  return "warm";
}
function fuelToneOf(pct: number): "green" | "yellow" | "red" {
  if (pct > 85) return "green";
  if (pct < 25) return "red";
  return "yellow";
}
const tyre = (c: number): TyreTemp => ({ value: `${Math.round(c)}°C`, tone: tyreTone(c) });

export function toLiveView(s: LiveSnapshot): LiveView {
  const f = s.frame;
  const d = s.derived;
  const delta = s.enrichment.delta_to_best_ns;
  return {
    status: s.status,
    speed: f ? `${Math.round(f.car_speed)}` : PLACEHOLDER,
    gear: f ? `${f.current_gear}` : PLACEHOLDER,
    rpm: f ? `${Math.round(f.rpm)}` : PLACEHOLDER,
    lap: f ? `${f.current_lap}/${f.total_laps}` : PLACEHOLDER,
    position: f ? `P${f.current_position}/${f.total_positions}` : PLACEHOLDER,
    fuelPct: d ? `${Math.round(d.fuel_pct)}%` : PLACEHOLDER,
    currentLapTime: d ? nsToClock(d.current_lap_time_ns) : PLACEHOLDER,
    delta: f && delta !== null ? nsToDeltaSeconds(delta) : PLACEHOLDER, // gated by frame: idle -> placeholder
    bestLap: f && f.best_lap !== null ? nsToClock(parseDuration(f.best_lap)) : PLACEHOLDER,
    lastLap: f && f.last_lap !== null ? nsToClock(parseDuration(f.last_lap)) : PLACEHOLDER,
    deltaTone: delta === null || delta === 0 || !f ? "none" : delta < 0 ? "blue" : "red",
    fuelTone: d ? fuelToneOf(d.fuel_pct) : "none",
    tyreTemps: f
      ? { fl: tyre(f.tyre_temp_fl), fr: tyre(f.tyre_temp_fr), rl: tyre(f.tyre_temp_rl), rr: tyre(f.tyre_temp_rr) }
      : { fl: { value: PLACEHOLDER, tone: "none" }, fr: { value: PLACEHOLDER, tone: "none" },
          rl: { value: PLACEHOLDER, tone: "none" }, rr: { value: PLACEHOLDER, tone: "none" } },
  };
}

/** Staleness is a display concern: true when served_at is older than threshold.
 *  An unparseable served_at is treated as stale (fail-safe: never show a bad timestamp as fresh). */
export function isStale(s: LiveSnapshot, nowMs: number, thresholdMs = 3000): boolean {
  const served = Date.parse(s.served_at);
  if (Number.isNaN(served)) return true;
  return nowMs - served > thresholdMs;
}
