/**
 * lapSectors.ts — derive a completed lap's final sector splits by replaying its stored
 * spatial samples through a fresh SectorTimer. No extra persistence: history already keeps
 * TracePoint[] per completed lap; this just re-runs the same live timing logic over it so
 * selecting "Lap N" in the UI can show Lap N's sectors.
 *
 * Caveat: after a page refresh the rehydrated history is DECIMATED (dump() thins to ~1500
 * samples/lap), so a replayed crossing can land on a slightly later point than the live timer
 * saw -- the stored-replay splits are approximate (the card is already labelled "approx"). Live,
 * same-session `completed[]` is full-density, so replay is frame-faithful there.
 */

import type { DrawGeometry } from "./rkbGeometry";
import { SectorTimer, type SectorTime } from "./sectorTimer";

export type LapPoint = { posX: number; posZ: number; t?: number };

export function lapSectors(points: ReadonlyArray<LapPoint>, geometry: DrawGeometry | null): SectorTime[] | null {
  const t = new SectorTimer(geometry);
  if (!t.hasPartition()) return null;
  let last: SectorTime[] | null = null;
  for (const p of points) {
    if (!Number.isFinite(p.posX) || !Number.isFinite(p.posZ)) continue;
    last = t.update({ sessionKey: "replay", lap: 1, lapTimeNs: Math.round((p.t ?? 0) * 1e9), x: p.posX, z: p.posZ });
  }
  // No usable points (empty, or all non-finite): still surface the initial all-null splits
  // rather than null, since a valid partition exists. NaN x/z can never register a sector
  // crossing (projectToClArc's nearest-vertex scan never beats its Infinity seed), so this
  // is a pure read of the timer's initial state.
  return last ?? t.update({ sessionKey: "replay", lap: 1, lapTimeNs: 0, x: NaN, z: NaN });
}
