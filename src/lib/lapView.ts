// src/lib/lapView.ts
// A read-only view over one lap's samples, exposing the same accessors the consumers used to call on
// historyStore (points / columns / frameAt). Two builders feed it: `lapViewFromSamples` for the live
// in-session cache (full-fidelity Sample rows), and `lapViewFromTrace` for a backend-served completed
// lap (the single snake_case -> camelCase remap point). This lets TrackTraceCanvas / TelemetryGraph /
// the scrub read a fetched lap without reaching into the imperative live store.
import type { Channel, TracePoint, FrameValues } from "./historyStore";
import type { TraceSampleDTO, LapDTO, PredictedSampleDTO } from "../contract/session";
import type { SectorTime } from "./sectorTimer";

// A full-channel lap row: structurally identical to historyStore's internal Sample, so the live
// cache can hand its Samples straight to `lapViewFromSamples`. `distance` is cumulative in-lap
// metres (0 for live-cache rows, which carry no distance); used to derive the track-fraction axis.
export type LapSample = { t: number; posX: number; posZ: number; surface: string; distance: number } & Record<Channel, number>;

export type LapView = {
  readonly points: readonly TracePoint[];
  readonly length: number;
  /** [xs, ...perChannel] where xs is lap-time seconds — same shape as historyStore.columns. */
  columns(chs: Channel[]): number[][];
  /** Per-sample track fraction in [0,1] (distance / total distance); index-ratio fallback when
   *  distance is unavailable (live-cache rows). Used to align median/reference overlays. */
  fractions(): number[];
  /** FrameValues at sample i (shape shared with liveView.frameToDisplay); null out of range. */
  frameAt(i: number): FrameValues | null;
};

function make(samples: LapSample[]): LapView {
  return {
    points: samples as readonly TracePoint[], // Sample is a structural superset of TracePoint
    length: samples.length,
    columns(chs) {
      const xs = samples.map(s => s.t);
      return [xs, ...chs.map(c => samples.map(s => s[c]))];
    },
    fractions() {
      const last = samples.length - 1;
      if (last < 1) return samples.map(() => 0);
      const total = samples[last].distance;
      // Only trust distance-derived fractions when the axis is sane (finite, positive total, all
      // finite, nondecreasing); otherwise fall back to an even index ratio so overlays/markers don't
      // silently misalign on a distance reset or jitter.
      const finite = Number.isFinite(total) && total > 0 && samples.every(s => Number.isFinite(s.distance));
      if (!finite) return samples.map((_, i) => i / last);
      let prev = -Infinity, mono = true;
      const out = samples.map(s => {
        if (s.distance < prev) mono = false;
        prev = s.distance;
        return Math.min(1, Math.max(0, s.distance / total));
      });
      return mono ? out : samples.map((_, i) => i / last);
    },
    frameAt(i) {
      const s = samples[i];
      if (!s) return null;
      return {
        t: s.t, fuel: s.fuel,
        tyreFL: s.tyreFL, tyreFR: s.tyreFR, tyreRL: s.tyreRL, tyreRR: s.tyreRR,
        delta: s.delta, speed: s.speed, gear: s.gear, rpm: s.rpm, throttle: s.throttle, brake: s.brake,
      };
    },
  };
}

/** In-session lap from the live cache (keeps every channel + surface). */
export const lapViewFromSamples = (samples: LapSample[]): LapView => make(samples);

/** Remap a backend trace to LapSample rows. The single place snake_case wire fields become
 *  camelCase samples — exposed so callers can both build a view AND warm the lapCache with it. */
export const traceToSamples = (dtos: TraceSampleDTO[]): LapSample[] => dtos.map(dtoToSample);

/** Backend-served completed lap. */
export const lapViewFromTrace = (dtos: TraceSampleDTO[]): LapView => make(traceToSamples(dtos));

function dtoToSample(d: TraceSampleDTO): LapSample {
  return {
    t: d.t_ns / 1e9, posX: d.pos_x, posZ: d.pos_z, surface: d.surface_type, // D-B30: surface now served
    distance: d.distance,
    throttle: d.throttle, brake: d.brake, clutch: d.clutch, rpm: d.rpm,
    gear: d.gear, speed: d.speed, boost: d.boost,
    delta: d.delta_to_best_ns == null ? NaN : d.delta_to_best_ns / 1e9,
    tyreFL: d.tyre_temp_fl, tyreFR: d.tyre_temp_fr, tyreRL: d.tyre_temp_rl, tyreRR: d.tyre_temp_rr,
    fuel: d.fuel,
  };
}

/** Map the predicted (reference) lap's samples to LapSample rows — for drawing the reference
 *  drive-line on the track canvas (pos_x/pos_z) and reading its per-channel values. */
export const predictedToSamples = (samples: PredictedSampleDTO[]): LapSample[] =>
  samples.map(d => ({
    t: d.t_ns / 1e9, posX: d.pos_x, posZ: d.pos_z, surface: "", distance: d.distance,
    throttle: d.throttle, brake: d.brake, clutch: d.clutch, rpm: d.rpm,
    gear: d.gear, speed: d.speed, boost: d.boost,
    delta: d.delta_to_best_ns == null ? NaN : d.delta_to_best_ns / 1e9,
    tyreFL: d.tyre_temp_fl, tyreFR: d.tyre_temp_fr, tyreRL: d.tyre_temp_rl, tyreRR: d.tyre_temp_rr,
    fuel: d.fuel,
  }));

/** Linear-interpolate values sampled on a sorted `grid` (fractions in [0,1]) at each of `targets`.
 *  Targets outside the grid clamp to the endpoints. Empty grid -> all NaN (nothing to draw). */
export function resampleByFraction(grid: number[], values: number[], targets: number[]): number[] {
  const n = grid.length;
  // Guard a backend length mismatch: interpolating past `values` would silently yield NaN. Bail to
  // an all-NaN column (uPlot renders it as a gap — nothing drawn) rather than a half-populated line.
  if (n === 0 || values.length !== n) return targets.map(() => NaN);
  let j = 0;
  return targets.map(f => {
    if (f <= grid[0]) return values[0];
    if (f >= grid[n - 1]) return values[n - 1];
    while (j < n - 1 && grid[j + 1] < f) j++;      // advance to the segment containing f
    while (j > 0 && grid[j] > f) j--;              // (targets need not be monotonic) walk back if needed
    const g0 = grid[j], g1 = grid[j + 1];
    const w = g1 > g0 ? (f - g0) / (g1 - g0) : 0;
    return values[j] + w * (values[j + 1] - values[j]);
  });
}

/** Adapt a lap's backend sector splits to the RightPanel's SectorTime[]. deltaMs is vs session best
 *  (backend semantics), not lap-over-lap. */
export function lapDtoToSectorTimes(lap: LapDTO): SectorTime[] {
  return lap.sectors.splits.map(s => ({
    sector: s.label,
    timeMs: s.duration_ns / 1e6,
    deltaMs: s.delta_to_best_ns == null ? null : s.delta_to_best_ns / 1e6,
    fastest: s.delta_to_best_ns === 0, // 0 => this lap owns (is session-fastest in) this sector
  }));
}
