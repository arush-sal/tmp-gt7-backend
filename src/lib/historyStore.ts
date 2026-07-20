import type { LiveSnapshot, Frame, Derived, Enrichment } from "../contract/live";

export type Channel =
  | "throttle" | "brake" | "clutch" | "rpm" | "gear" | "speed" | "boost" | "delta"
  | "tyreFL" | "tyreFR" | "tyreRL" | "tyreRR" | "fuel";

// t = seconds since lap start; delta may be NaN. posX/posZ/surface are spatial-only fields:
// kept OUT of Channel/columns() so P3 uPlot never sees them (see trace() below).
type Sample = { t: number; posX: number; posZ: number; surface: string } & Record<Channel, number>;

export type TracePoint = {
  readonly posX: number; readonly posZ: number;
  readonly throttle: number; readonly brake: number; readonly surface: string;
  // Lap-time seconds of this sample. Present at runtime (trace() returns Samples), used to
  // measure the REAL elapsed time between decimated/thinned points for the teleport guard.
  // Optional so lightweight test fixtures need not supply it (they fall back to ~60Hz).
  readonly t?: number;
};

function project(f: Frame, d: Derived, e: Enrichment): Sample {
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

/** Serializable snapshot of the store for cross-refresh persistence (see dump/load). */
export type PersistedHistory = {
  v: 1;
  session: string | undefined;
  lap: number | null;
  totalLaps: number | null;
  completed: Sample[][];
  buf: Sample[];
};

export type HistoryStore = {
  push(s: LiveSnapshot): void;
  /** Gate append: while not live (paused/idle/race-over), push() is a no-op. Default true. */
  setLive(live: boolean): void;
  columns(chs: Channel[]): number[][];
  medianColumn(ch: Channel): number[];
  size(): number;
  completedLapCount(): number;
  lastLapFuelUsedPct(): number | null;
  reset(): void;
  trace(): { completed: readonly (readonly TracePoint[])[]; current: readonly TracePoint[] };
  revision(): number;
  lapColumns(i: number, chs: Channel[]): number[][] | null;
  lapSampleCount(i: number): number;
  /** Decimated, lap-capped serializable state for persistence; null when empty. */
  dump(): PersistedHistory | null;
  /** Rehydrate from a dump()'d payload (best-effort; ignores anything malformed). */
  load(data: unknown): void;
};

const MAX_COMPLETED_LAPS = 20;
const DEFAULT_MAX_SAMPLES_PER_LAP = 36_000; // ~10 min at 60Hz — safety bound for a non-terminating lap

// Persistence bounds: keep localStorage small. A lap is thinned to at most this many
// samples (endpoints preserved; median interpolates, trace stays visually faithful), and
// only the most recent laps are persisted.
export const PERSIST_TARGET_SAMPLES = 1500;
export const PERSIST_MAX_LAPS = 8;

// Uniform-stride thinning that always keeps the first and last sample.
function thin(lap: Sample[], target: number): Sample[] {
  if (lap.length <= target) return lap.slice();
  const stride = Math.ceil(lap.length / target);
  const out: Sample[] = [];
  for (let i = 0; i < lap.length; i += stride) out.push(lap[i]);
  const last = lap[lap.length - 1];
  if (out[out.length - 1] !== last) out.push(last);
  return out;
}

function isSample(x: unknown): x is Sample {
  return !!x && typeof x === "object" && typeof (x as Sample).t === "number";
}

export function createHistoryStore(maxSamplesPerLap = DEFAULT_MAX_SAMPLES_PER_LAP): HistoryStore {
  let buf: Sample[] = [];
  let completed: Sample[][] = [];
  let lap: number | null = null;
  let session: string | undefined; // informational only (dump/load); push no longer resets on it
  let totalLaps: number | null = null;
  let live = true;

  // Median cache: values change only when the completed-lap set OR the scope (totalLaps) changes.
  // `version` bumps then; per-x medians are reused across rAF frames otherwise.
  let version = 0;
  const medCache = new Map<Channel, Map<number, number>>();
  let medCacheVersion = -1;
  const bump = () => { version++; };

  // revision: dirty-redraw counter for the trace canvas. Distinct from `version` (median cache):
  // bumps on every accepted push, session reset, lap rollover, and reset().
  let revision = 0;
  const pruneMedCacheX = (t: number) => { for (const m of medCache.values()) m.delete(t); };

  const scopeLaps = (): Sample[][] => {
    if (totalLaps !== null && totalLaps >= 1 && totalLaps <= 20) return completed;
    return completed.slice(-5); // >20, 0, or null (unlimited/unknown)
  };

  return {
    setLive(v) { live = v; },
    push(s) {
      if (!live) return; // paused/idle/race-over: no-op, short-circuit before any mutation
      if (s.status !== "live" || !s.frame || !s.derived) return;
      // Session reset is owned externally by reset() (called by the race controller on a new
      // race); push() no longer wipes on session_id change. See task C1.
      if (lap !== null && s.frame.current_lap !== lap) {
        if (buf.length) {
          completed.push(buf);                 // full lap (no time-eviction happened)
          if (completed.length > MAX_COMPLETED_LAPS) completed.shift();
        }
        buf = [];
        bump(); // completed-lap set changed -> invalidate median cache
        revision++;
      }
      lap = s.frame.current_lap;
      if (s.frame.total_laps !== totalLaps) { totalLaps = s.frame.total_laps; bump(); } // scope change -> invalidate
      const sample = project(s.frame, s.derived, s.enrichment);
      // x must be strictly increasing (uPlot aligned-data contract).
      if (buf.length && sample.t <= buf[buf.length - 1].t) return;
      buf.push(sample);
      revision++;
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
    lastLapFuelUsedPct() {
      if (completed.length === 0) return null;
      const last = completed[completed.length - 1];
      if (last.length === 0) return null;
      const used = last[0].fuel - last[last.length - 1].fuel;
      if (!Number.isFinite(used) || used <= 0) return null;
      return used;
    },
    reset() { buf = []; completed = []; lap = null; session = undefined; totalLaps = null; bump(); revision++; },
    trace() {
      // Sample is structurally a superset of TracePoint -> zero-copy narrowing, no remap.
      // Returns LIVE internal arrays: read + draw synchronously each call; do NOT retain the
      // result across frames (a later push mutates buf/completed in place). Use revision() as
      // the dirty flag to know when to re-call and redraw.
      return { completed, current: buf } as {
        completed: readonly (readonly TracePoint[])[]; current: readonly TracePoint[];
      };
    },
    revision() { return revision; },
    lapColumns(i, chs) {
      const lap = completed[i];
      if (!lap) return null;
      const xs = lap.map(s => s.t);
      return [xs, ...chs.map(c => lap.map(s => s[c]))];
    },
    lapSampleCount(i) {
      const lap = completed[i];
      return lap ? lap.length : 0;
    },
    dump() {
      if (buf.length === 0 && completed.length === 0) return null;
      return {
        v: 1,
        session, lap, totalLaps,
        completed: completed.slice(-PERSIST_MAX_LAPS).map(l => thin(l, PERSIST_TARGET_SAMPLES)),
        buf: thin(buf, PERSIST_TARGET_SAMPLES),
      };
    },
    load(data) {
      const d = data as Partial<PersistedHistory> | null;
      if (!d || d.v !== 1 || !Array.isArray(d.completed) || !Array.isArray(d.buf)) return;
      // Best-effort: keep only well-formed sample arrays; a malformed payload rehydrates
      // to whatever survives rather than throwing.
      completed = d.completed.filter(Array.isArray).map(l => (l as unknown[]).filter(isSample));
      buf = (d.buf as unknown[]).filter(isSample);
      session = typeof d.session === "string" ? d.session : undefined;
      lap = typeof d.lap === "number" ? d.lap : null;
      totalLaps = typeof d.totalLaps === "number" ? d.totalLaps : null;
      bump();
      revision++;
    },
  };
}
