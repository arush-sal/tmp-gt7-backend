import type { LiveSnapshot, Frame, Derived, Enrichment } from "../contract/live";

export type Channel =
  | "throttle" | "brake" | "clutch" | "rpm" | "gear" | "speed" | "boost" | "delta"
  | "tyreFL" | "tyreFR" | "tyreRL" | "tyreRR" | "fuel";

type Sample = { t: number } & Record<Channel, number>; // t = seconds since lap start; delta may be NaN

function project(f: Frame, d: Derived, e: Enrichment): Sample {
  return {
    t: d.current_lap_time_ns / 1e9,
    throttle: f.throttle, brake: f.brake, clutch: f.clutch, rpm: f.rpm,
    gear: f.current_gear, speed: f.car_speed, boost: f.boost,
    delta: e.delta_to_best_ns === null ? NaN : e.delta_to_best_ns / 1e9,
    tyreFL: f.tyre_temp_fl, tyreFR: f.tyre_temp_fr, tyreRL: f.tyre_temp_rl, tyreRR: f.tyre_temp_rr,
    fuel: d.fuel_pct,
  };
}

// Median value of the finite numbers in `xs`, or NaN if none are finite.
function median(xs: number[]): number {
  const f = xs.filter(Number.isFinite).sort((a, b) => a - b);
  if (f.length === 0) return NaN;
  const mid = f.length >> 1;
  return f.length % 2 ? f[mid] : (f[mid - 1] + f[mid]) / 2;
}

// Linear interpolation of a completed lap's channel value at lap-time x.
// Outside the lap's [first,last] range -> NaN (no extrapolation). Single-sample lap -> that
// point's value only at exactly its t, else NaN (no zero-length-segment divide).
function interpAt(lap: Sample[], ch: Channel, x: number): number {
  const n = lap.length;
  if (n === 0) return NaN;
  if (x < lap[0].t || x > lap[n - 1].t) return NaN;
  // binary search for the last index with t <= x
  let lo = 0, hi = n - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (lap[mid].t <= x) lo = mid; else hi = mid - 1;
  }
  const a = lap[lo];
  if (a.t === x || lo === n - 1) return a[ch];
  const b = lap[lo + 1];
  const span = b.t - a.t;
  if (span <= 0) return a[ch]; // degenerate/duplicate timestamps — no divide-by-zero
  const frac = (x - a.t) / span;
  return a[ch] + (b[ch] - a[ch]) * frac;
}

export type HistoryStore = {
  push(s: LiveSnapshot): void;
  columns(chs: Channel[]): number[][];
  medianColumn(ch: Channel): number[];
  size(): number;
  completedLapCount(): number;
  reset(): void;
};

const MAX_COMPLETED_LAPS = 20;
const DEFAULT_MAX_SAMPLES_PER_LAP = 36_000; // ~10 min at 60Hz — safety bound for a non-terminating lap

export function createHistoryStore(maxSamplesPerLap = DEFAULT_MAX_SAMPLES_PER_LAP): HistoryStore {
  let buf: Sample[] = [];
  let completed: Sample[][] = [];
  let lap: number | null = null;
  let session: string | undefined;
  let totalLaps: number | null = null;

  // Median cache: values change only when the completed-lap set OR the scope (totalLaps) changes.
  // `version` bumps then; per-x medians are reused across rAF frames otherwise.
  let version = 0;
  const medCache = new Map<Channel, Map<number, number>>();
  let medCacheVersion = -1;
  const bump = () => { version++; };
  const pruneMedCacheX = (t: number) => { for (const m of medCache.values()) m.delete(t); };

  const scopeLaps = (): Sample[][] => {
    if (totalLaps !== null && totalLaps >= 1 && totalLaps <= 20) return completed;
    return completed.slice(-5); // >20, 0, or null (unlimited/unknown)
  };

  return {
    push(s) {
      if (s.status !== "live" || !s.frame || !s.derived) return;
      const sid = s.enrichment.session_id;
      if (sid !== session) { session = sid; buf = []; completed = []; lap = null; totalLaps = null; bump(); }
      if (lap !== null && s.frame.current_lap !== lap) {
        if (buf.length) {
          completed.push(buf);                 // full lap (no time-eviction happened)
          if (completed.length > MAX_COMPLETED_LAPS) completed.shift();
        }
        buf = [];
        bump(); // completed-lap set changed -> invalidate median cache
      }
      lap = s.frame.current_lap;
      if (s.frame.total_laps !== totalLaps) { totalLaps = s.frame.total_laps; bump(); } // scope change -> invalidate
      const sample = project(s.frame, s.derived, s.enrichment);
      // x must be strictly increasing (uPlot aligned-data contract).
      if (buf.length && sample.t <= buf[buf.length - 1].t) return;
      buf.push(sample);
      // Full-lap retention: NO time-window eviction (median compares whole laps). A generous
      // sample cap bounds a pathological non-terminating lap; drop-oldest + prune its dead
      // median-cache entry so medCache can't outgrow buf.
      while (buf.length > maxSamplesPerLap) { const dropped = buf.shift()!; pruneMedCacheX(dropped.t); }
    },
    columns(chs) {
      const xs = buf.map(s => s.t);
      return [xs, ...chs.map(c => buf.map(s => s[c]))];
    },
    medianColumn(ch) {
      if (medCacheVersion !== version) { medCache.clear(); medCacheVersion = version; }
      const laps = scopeLaps();
      if (laps.length < 2) return buf.map(() => NaN);
      let cache = medCache.get(ch);
      if (!cache) { cache = new Map(); medCache.set(ch, cache); }
      return buf.map(s => {
        const hit = cache!.get(s.t);
        if (hit !== undefined) return hit;
        const v = median(laps.map(l => interpAt(l, ch, s.t)));
        cache!.set(s.t, v);
        return v;
      });
    },
    size() { return buf.length; },
    completedLapCount() { return completed.length; },
    reset() { buf = []; completed = []; lap = null; session = undefined; totalLaps = null; bump(); },
  };
}
