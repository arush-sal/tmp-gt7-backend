import type { LiveSnapshot } from "../contract/live";
import type { FrameValues } from "./historyStore";
import { nsToClock, nsToDeltaSeconds, parseDuration, PLACEHOLDER } from "./format";

// Tyre-temperature bands (grip-window semantics, not raw hot/cold):
//   under   (< 75 C)      -> red   : tyres below the working window, low grip
//   optimal (75..90 C)    -> green : in the working window
//   over    (> 90 C)      -> lime  : above the window, overheating
export type TyreTemp = { value: string; tone: "under" | "optimal" | "over" | "none" };

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
  predicted: string;     // predicted-best lap (backend analysis.predicted_best_ns); "—" until predicted
  bestLap: string;
  lastLap: string;
  tyreTemps: { fl: TyreTemp; fr: TyreTemp; rl: TyreTemp; rr: TyreTemp };
  deltaTone: "blue" | "red" | "none";
  fuelTone: "green" | "yellow" | "red" | "none";
  // format-C CONFIRMED fields; PLACEHOLDER on format-A / older sources that omit them
  steering: string;      // averaged front-wheel steer angle, degrees (e.g. "+6.9°")
  surface: string;       // per-wheel surface codes FL,FR,RL,RR (e.g. "TTTT")
  carCategory: string;   // GT7 category code (e.g. "GRN")
};

// bands (see TyreTemp): under < 75 <= optimal <= 90 < over
function tyreTone(c: number): "under" | "optimal" | "over" {
  if (c < 75) return "under";
  if (c > 90) return "over";
  return "optimal";
}
function fuelToneOf(pct: number): "green" | "yellow" | "red" {
  if (pct > 85) return "green";
  if (pct < 25) return "red";
  return "yellow";
}
const tyre = (c: number): TyreTemp => ({ value: `${Math.round(c)}°C`, tone: tyreTone(c) });

// Predicted-best lap from ns; PLACEHOLDER when absent/null or 0 (backend emits 0 = no prediction
// yet). Shared by the live view and the post-race /coaching fetch so both format identically.
export function predictedOf(ns: number | null | undefined): string {
  return typeof ns === "number" && ns > 0 ? nsToClock(ns) : PLACEHOLDER;
}

// Averaged front-wheel steer angle in degrees, e.g. "+6.9°"; PLACEHOLDER unless both wheels present.
function steeringOf(fl?: number, fr?: number): string {
  if (fl === undefined || fr === undefined) return PLACEHOLDER;
  const deg = ((fl + fr) / 2) * (180 / Math.PI);
  return `${deg >= 0 ? "+" : "-"}${Math.abs(deg).toFixed(1)}°`;
}

export function toLiveView(s: LiveSnapshot): LiveView {
  const f = s.frame;
  const d = s.derived;
  const delta = s.enrichment.delta_to_best_ns;
  return {
    status: s.status,
    speed: f ? `${Math.round(f.car_speed)}` : PLACEHOLDER,
    gear: f ? `${f.current_gear}` : PLACEHOLDER,
    rpm: f ? `${Math.round(f.rpm)}` : PLACEHOLDER,
    lap: f
      ? `${f.total_laps > 0 ? Math.min(f.current_lap, f.total_laps) : f.current_lap}/${f.total_laps}`
      : PLACEHOLDER,
    position: f ? `P${f.current_position}/${f.total_positions}` : PLACEHOLDER,
    fuelPct: d ? `${Math.round(d.fuel_pct)}%` : PLACEHOLDER,
    currentLapTime: d ? nsToClock(d.current_lap_time_ns) : PLACEHOLDER,
    delta: f && delta !== null ? nsToDeltaSeconds(delta) : PLACEHOLDER, // gated by frame: idle -> placeholder
    predicted: predictedOf(s.enrichment.analysis?.predicted_best_ns),
    bestLap: f && f.best_lap !== null ? nsToClock(parseDuration(f.best_lap)) : PLACEHOLDER,
    lastLap: f && f.last_lap !== null ? nsToClock(parseDuration(f.last_lap)) : PLACEHOLDER,
    deltaTone: delta === null || delta === 0 || !f ? "none" : delta < 0 ? "blue" : "red",
    fuelTone: d ? fuelToneOf(d.fuel_pct) : "none",
    steering: f ? steeringOf(f.wheel_steering_angle_front_left, f.wheel_steering_angle_front_right) : PLACEHOLDER,
    surface: f && f.surface_type ? f.surface_type : PLACEHOLDER,
    carCategory: f && f.car_category ? f.car_category : PLACEHOLDER,
    tyreTemps: f
      ? { fl: tyre(f.tyre_temp_fl), fr: tyre(f.tyre_temp_fr), rl: tyre(f.tyre_temp_rl), rr: tyre(f.tyre_temp_rr) }
      : { fl: { value: PLACEHOLDER, tone: "none" }, fr: { value: PLACEHOLDER, tone: "none" },
          rl: { value: PLACEHOLDER, tone: "none" }, rr: { value: PLACEHOLDER, tone: "none" } },
  };
}

// The subset of LiveView fields that a scrubbed stored frame (FrameValues, from
// historyStore.frameAt) can populate — position/lap-counter/best/last stay from live `view`.
export type FrameDisplay = Pick<LiveView, "currentLapTime" | "fuelPct" | "fuelTone" | "tyreTemps" | "delta" | "deltaTone">;

// Rehydrated (post-refresh, decimated) frames can carry a non-finite value -- guard rather
// than render NaN/Infinity.
const tyreOrPlaceholder = (c: number): TyreTemp => (Number.isFinite(c) ? tyre(c) : { value: PLACEHOLDER, tone: "none" });

export function frameToDisplay(fr: FrameValues): FrameDisplay {
  const fuelOk = Number.isFinite(fr.fuel);
  return {
    currentLapTime: nsToClock(Math.round(fr.t * 1e9)),
    fuelPct: fuelOk ? `${Math.round(fr.fuel)}%` : PLACEHOLDER,
    fuelTone: fuelOk ? fuelToneOf(fr.fuel) : "none",
    tyreTemps: {
      fl: tyreOrPlaceholder(fr.tyreFL), fr: tyreOrPlaceholder(fr.tyreFR),
      rl: tyreOrPlaceholder(fr.tyreRL), rr: tyreOrPlaceholder(fr.tyreRR),
    },
    delta: !Number.isFinite(fr.delta) ? PLACEHOLDER : nsToDeltaSeconds(Math.round(fr.delta * 1e9)),
    deltaTone: !Number.isFinite(fr.delta) || fr.delta === 0 ? "none" : fr.delta < 0 ? "blue" : "red",
  };
}

/** Staleness is a display concern: true when served_at is older than threshold.
 *  An unparseable served_at is treated as stale (fail-safe: never show a bad timestamp as fresh). */
export function isStale(s: LiveSnapshot, nowMs: number, thresholdMs = 3000): boolean {
  const served = Date.parse(s.served_at);
  if (Number.isNaN(served)) return true;
  return nowMs - served > thresholdMs;
}
