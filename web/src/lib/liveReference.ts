// Reference overlay for the LIVE current lap. The live store's samples carry no lap distance
// (historyStore project() sets distance 0), so a track-fraction alignment would degrade to an index
// ratio. The live frame does carry world position (position_x/z) and the reference carries pos_x/pos_z
// + fraction per sample, so each live sample is projected onto the reference line (nearest reference
// point) to get its track fraction; the reference channel value is then interpolated at that fraction.
// Where the live position is unusable (NaN), it falls back to a time-aligned "ghost": the live lap
// time looked up in the reference's t_ns.
//
// Cost: the nearest-point search is windowed around the previous match (a car advances monotonically),
// with a global scan only for the first sample or after losing lock (spin/teleport), so O(window) per
// NEW sample; results are cached and only new samples are processed per frame.
import type { Channel, TracePoint } from "./historyStore";
import type { ReferenceDTO } from "../contract/session";
import { referenceValues } from "./lapOverlays";

const BACK = 30, AHEAD = 400;       // search window (reference samples) around the last match
const LOST_M2 = 100 * 100;          // m^2: a window miss farther than this triggers a global rescan

export type LiveRefAligner = {
  /** Reference column for `channel`, one value per live sample; null if the reference lacks the channel. */
  column(channel: Channel, trace: readonly TracePoint[]): number[] | null;
};

// Linear interpolation of `vals` at x over an ascending `xs` (clamped), via binary search.
function interp(xs: number[], vals: number[], x: number): number {
  const n = xs.length;
  if (x <= xs[0]) return vals[0];
  if (x >= xs[n - 1]) return vals[n - 1];
  let lo = 0, hi = n - 1;
  while (hi - lo > 1) { const m = (lo + hi) >> 1; if (xs[m] <= x) lo = m; else hi = m; }
  const w = xs[hi] > xs[lo] ? (x - xs[lo]) / (xs[hi] - xs[lo]) : 0;
  return vals[lo] + w * (vals[hi] - vals[lo]);
}

export function createLiveRefAligner(reference: ReferenceDTO): LiveRefAligner {
  const S = reference.samples;
  const n = S.length;
  const fr = S.map(s => s.fraction);
  const tSec = S.map(s => s.t_ns / 1e9);
  const px = S.map(s => s.pos_x), pz = S.map(s => s.pos_z);
  const vals = new Map<Channel, number[] | null>();

  // Per-lap alignment cache: fractions per live sample + the buffer identity it was computed for.
  let fracs: number[] = [];
  let firstRef: TracePoint | null = null;
  let last = -1;
  const cols = new Map<Channel, number[]>();

  const nearest = (x: number, z: number, from: number, to: number) => {
    let best = -1, bd = Infinity;
    for (let i = from; i <= to; i++) {
      const dx = px[i] - x, dz = pz[i] - z, d = dx * dx + dz * dz;
      if (d < bd) { bd = d; best = i; }
    }
    return { best, bd };
  };

  const fractionOf = (p: TracePoint): number => {
    if (Number.isFinite(p.posX) && Number.isFinite(p.posZ)) {
      let r = last < 0 ? nearest(p.posX, p.posZ, 0, n - 1)
        : nearest(p.posX, p.posZ, Math.max(0, last - BACK), Math.min(n - 1, last + AHEAD));
      if (r.bd > LOST_M2) r = nearest(p.posX, p.posZ, 0, n - 1);
      last = r.best;
      return fr[r.best];
    }
    if (p.t !== undefined && Number.isFinite(p.t)) return interp(tSec, fr, p.t);
    return last >= 0 ? fr[last] : 0;
  };

  return {
    column(channel, trace) {
      if (!n) return null;
      if (!vals.has(channel)) vals.set(channel, referenceValues(reference, channel));
      const v = vals.get(channel);
      if (!v) return null;
      // A new lap / reset / drop-oldest changes the buffer's head or shrinks it: start over.
      if (trace.length < fracs.length || trace[0] !== firstRef) {
        fracs = []; cols.clear(); last = -1; firstRef = trace[0] ?? null;
      }
      for (let i = fracs.length; i < trace.length; i++) fracs.push(fractionOf(trace[i]));
      let col = cols.get(channel);
      if (!col) { col = []; cols.set(channel, col); }
      for (let i = col.length; i < fracs.length; i++) col.push(interp(fr, v, fracs[i]));
      return col;
    },
  };
}
