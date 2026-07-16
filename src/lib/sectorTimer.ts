/**
 * sectorTimer.ts — stateful live sector-split timer, built on the pure partition
 * helpers in sectorTiming.ts. One instance per track; feed it every live snapshot via
 * update() and it reports the running (or just-completed) lap's sector splits.
 *
 * Limitation: sector-crossing detection is nearest-vertex projection onto the
 * centerline, and jitter is guarded only by requiring the crossing to advance to the
 * IMMEDIATE next sector (a jump of more than one sector is treated as noise and
 * ignored, not backfilled). Very short sectors, or big position jumps (e.g. a reset/
 * teleport), can therefore miss a split rather than mis-record one -- deliberately the
 * safer failure mode for a display that is already labelled "approx".
 */

import type { DrawGeometry } from "./rkbGeometry";
import { clArcLengths, projectToClArc, sectorBoundaries, sectorIndexAt, type SectorPartition } from "./sectorTiming";

export type SectorTime = { sector: string; timeMs: number | null };

export type SectorTimerInput = {
  /** Identifies the current session/lap-count context; changing it forces a reset even mid-lap. */
  sessionKey: string | null;
  lap: number;
  lapTimeNs: number;
  x: number;
  z: number;
};

export class SectorTimer {
  private readonly cl: ReadonlyArray<[number, number]>;
  private readonly arc: number[];
  private readonly partition: SectorPartition | null;

  private sessionKey: string | null = null;
  private lap: number | null = null;
  private currentIdx = 0;
  private entryLapTimeNs = 0;
  private lastLapTimeNs = 0;
  private times: Array<number | null>;

  constructor(geometry: DrawGeometry | null) {
    this.cl = geometry?.cl ?? [];
    this.partition = geometry
      ? sectorBoundaries(geometry.cl, geometry.sectors.map(s => ({ sector: s.label, x: s.x, z: s.z })))
      : null;
    this.arc = this.partition ? clArcLengths(this.cl) : [];
    this.times = this.partition ? this.partition.order.map(() => null) : [];
  }

  /** No geometry / fewer than 2 usable sector labels -> permanently null (caller keeps showing "pending"). */
  hasPartition(): boolean {
    return this.partition !== null;
  }

  private resetTimes() {
    this.currentIdx = 0;
    this.entryLapTimeNs = 0;
    this.times = this.partition ? this.partition.order.map(() => null) : [];
  }

  update(input: SectorTimerInput): SectorTime[] | null {
    if (!this.partition) return null;

    let justCompleted: Array<number | null> | null = null;

    if (input.sessionKey !== this.sessionKey) {
      this.sessionKey = input.sessionKey;
      this.lap = input.lap;
      this.lastLapTimeNs = 0;
      this.resetTimes();
    } else if (this.lap !== null && input.lap !== this.lap) {
      // Lap rollover: finalize the sector we were in using the last known lap time
      // from just before the wrap (this frame's lapTimeNs has already reset near 0).
      if (this.currentIdx < this.times.length) {
        this.times[this.currentIdx] = (this.lastLapTimeNs - this.entryLapTimeNs) / 1_000_000;
      }
      justCompleted = this.times;
      this.lap = input.lap;
      this.resetTimes();
    } else if (this.lap === null) {
      this.lap = input.lap;
    }

    const arcLen = projectToClArc(this.cl, this.arc, input.x, input.z);
    if (!Number.isNaN(arcLen)) {
      const idx = sectorIndexAt(arcLen, this.partition.bounds);
      if (idx === this.currentIdx + 1) {
        this.times[this.currentIdx] = (input.lapTimeNs - this.entryLapTimeNs) / 1_000_000;
        this.entryLapTimeNs = input.lapTimeNs;
        this.currentIdx = idx;
      }
    }

    this.lastLapTimeNs = input.lapTimeNs;

    const source = justCompleted ?? this.times;
    return this.partition.order.map((sector, i) => ({ sector, timeMs: source[i] }));
  }
}
