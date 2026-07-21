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
  let lastPt: LapPoint | null = null;
  for (const p of points) {
    if (!Number.isFinite(p.posX) || !Number.isFinite(p.posZ)) continue;
    last = t.update({ sessionKey: "replay", lap: 1, lapTimeNs: Math.round((p.t ?? 0) * 1e9), x: p.posX, z: p.posZ });
    lastPt = p;
  }
  if (lastPt) {
    // Feed one more update as a lap rollover (lap 2) to finalize the sector-in-progress --
    // SectorTimer only finalizes on rollover (see sectorTimer.ts `update`'s lap-change branch),
    // so without this the last sector (e.g. S4) never gets a timeMs. x/z are NaN (not the last
    // point's real position): after rollover resetLap() rewinds currentIdx to 0, and reusing a
    // real position can spuriously re-cross "sector 1" and clobber S1's already-correct time.
    // NaN doesn't misfire: projectToClArc(NaN) returns arc[0]=0 (every `NaN < bestDist` is false
    // so bestIdx stays 0), and sectorIndexAt(0) == currentIdx (0 after reset) reads as "no new
    // crossing" -- so only the rollover finalize runs. (Holds while bounds[0] > 0, true for any
    // normal partition.)
    last = t.update({ sessionKey: "replay", lap: 2, lapTimeNs: Math.round((lastPt.t ?? 0) * 1e9), x: NaN, z: NaN });
  }
  // No usable points (empty, or all non-finite): still surface the initial all-null splits
  // rather than null, since a valid partition exists. NaN x/z can't register a crossing (see
  // above: arcLen resolves to 0, which maps to the current sector, not the next), so this is a
  // pure read of the timer's initial state.
  return last ?? t.update({ sessionKey: "replay", lap: 1, lapTimeNs: 0, x: NaN, z: NaN });
}
