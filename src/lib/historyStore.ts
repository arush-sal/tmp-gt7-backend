import type { LiveSnapshot, Frame, Derived, Enrichment } from "../contract/live";
import type { LapSample } from "./lapView";

export type Channel =
  | "throttle" | "brake" | "clutch" | "rpm" | "gear" | "speed" | "boost" | "delta"
  | "tyreFL" | "tyreFR" | "tyreRL" | "tyreRR" | "fuel";

export type TracePoint = {
  readonly posX: number; readonly posZ: number;
  readonly throttle: number; readonly brake: number; readonly surface: string;
  // Lap-time seconds of this sample. Present at runtime (trace() returns LapSamples), used to
  // measure the REAL elapsed time between decimated/thinned points for the teleport guard.
  // Optional so lightweight test fixtures need not supply it (they fall back to ~60Hz).
  readonly t?: number;
};

// A completed lap's full-fidelity samples are handed to onLapComplete (→ lapCache) on rollover;
// the store itself no longer retains them. LapSample is structurally the store's row type.
function project(f: Frame, d: Derived, e: Enrichment): LapSample {
  return {
    t: d.current_lap_time_ns / 1e9,
    posX: f.position_x ?? NaN, posZ: f.position_z ?? NaN, surface: f.surface_type ?? "",
    throttle: f.throttle, brake: f.brake, clutch: f.clutch, rpm: f.rpm,
    gear: f.current_gear, speed: f.car_speed, boost: f.boost,
    delta: e.delta_to_best_ns === null ? NaN : e.delta_to_best_ns / 1e9,
    tyreFL: f.tyre_temp_fl, tyreFR: f.tyre_temp_fr, tyreRL: f.tyre_temp_rl, tyreRR: f.tyre_temp_rr,
    fuel: d.fuel_pct,
  };
}

export type FrameValues = {
  t: number;            // lap-time seconds of this frame
  fuel: number;         // fuel_pct 0..100
  tyreFL: number; tyreFR: number; tyreRL: number; tyreRR: number; // degC
  delta: number;        // seconds vs best; NaN if none
  speed: number; gear: number; rpm: number; throttle: number; brake: number;
};

export type HistoryStore = {
  push(s: LiveSnapshot): void;
  /** Gate append: while not live (paused/idle/race-over), push() is a no-op. Default true. */
  setLive(live: boolean): void;
  columns(chs: Channel[]): number[][];
  size(): number;
  completedLapCount(): number;
  lastLapFuelUsedPct(): number | null;
  reset(): void;
  /** Live current-lap trace only; completed laps live in the lapCache now (see onLapComplete). */
  trace(): { current: readonly TracePoint[] };
  revision(): number;
};

const DEFAULT_MAX_SAMPLES_PER_LAP = 36_000; // ~10 min at 60Hz — safety bound for a non-terminating lap

/**
 * Live current-lap ring buffer. On a lap rollover the finished buffer is handed to
 * `onLapComplete(lap, samples)` (the caller persists it to the session-scoped lapCache) and a
 * plain completed-lap counter increments; the store keeps no completed-lap arrays.
 */
export function createHistoryStore(
  onLapComplete?: (lap: number, samples: LapSample[]) => void,
  maxSamplesPerLap = DEFAULT_MAX_SAMPLES_PER_LAP,
): HistoryStore {
  let buf: LapSample[] = [];
  let lap: number | null = null;
  let totalLaps: number | null = null;
  let live = true;
  let completedCount = 0;
  let lastLapFuel: number | null = null;

  // revision: dirty-redraw counter for the trace canvas. Bumps on every accepted push, lap
  // rollover, and reset().
  let revision = 0;

  return {
    setLive(v) { live = v; },
    push(s) {
      if (!live) return; // paused/idle/race-over: no-op, short-circuit before any mutation
      if (s.status !== "live" || !s.frame || !s.derived) return;
      // Session reset is owned externally by reset() (called by the race controller on a new race).
      if (lap !== null && s.frame.current_lap !== lap) {
        if (buf.length) {
          onLapComplete?.(lap, buf); // hand the finished lap off before clearing
          completedCount++;
          const used = buf[0].fuel - buf[buf.length - 1].fuel;
          if (Number.isFinite(used) && used > 0) lastLapFuel = used; // else keep prev/null
        }
        buf = [];
        revision++;
      }
      lap = s.frame.current_lap;
      if (s.frame.total_laps !== totalLaps) totalLaps = s.frame.total_laps;
      const sample = project(s.frame, s.derived, s.enrichment);
      // x must be strictly increasing (uPlot aligned-data contract).
      if (buf.length && sample.t <= buf[buf.length - 1].t) return;
      buf.push(sample);
      revision++;
      // A generous sample cap bounds a pathological non-terminating lap; drop-oldest.
      while (buf.length > maxSamplesPerLap) buf.shift();
    },
    columns(chs) {
      const xs = buf.map(s => s.t);
      return [xs, ...chs.map(c => buf.map(s => s[c]))];
    },
    size() { return buf.length; },
    completedLapCount() {
      // Never report more than total_laps (GT7's post-race "11/10" cooldown would otherwise
      // inflate the count by one).
      if (totalLaps != null && totalLaps > 0 && completedCount > totalLaps) return completedCount - 1;
      return completedCount;
    },
    lastLapFuelUsedPct() { return lastLapFuel; },
    reset() { buf = []; completedCount = 0; lastLapFuel = null; lap = null; totalLaps = null; revision++; },
    trace() {
      // LapSample is structurally a superset of TracePoint -> zero-copy narrowing. Returns the
      // LIVE internal array: read + draw synchronously each call; use revision() as the dirty flag.
      return { current: buf } as { current: readonly TracePoint[] };
    },
    revision() { return revision; },
  };
}
