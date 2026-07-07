import type { LiveSnapshot } from "../contract/live";
import { nsToClock, nsToDeltaSeconds, parseDuration, PLACEHOLDER } from "./format";

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
  tyreTemps: { fl: string; fr: string; rl: string; rr: string };
};

const temp = (c: number) => `${Math.round(c)}°C`; // 96°C

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
    tyreTemps: f
      ? { fl: temp(f.tyre_temp_fl), fr: temp(f.tyre_temp_fr), rl: temp(f.tyre_temp_rl), rr: temp(f.tyre_temp_rr) }
      : { fl: PLACEHOLDER, fr: PLACEHOLDER, rl: PLACEHOLDER, rr: PLACEHOLDER },
  };
}

/** Staleness is a display concern: true when served_at is older than threshold.
 *  An unparseable served_at is treated as stale (fail-safe: never show a bad timestamp as fresh). */
export function isStale(s: LiveSnapshot, nowMs: number, thresholdMs = 3000): boolean {
  const served = Date.parse(s.served_at);
  if (Number.isNaN(served)) return true;
  return nowMs - served > thresholdMs;
}
