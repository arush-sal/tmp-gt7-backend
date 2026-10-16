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

export type SectorTime = {
  sector: string;
  /** Most recently completed time for this sector. Once set, it is only ever overwritten,
   *  never cleared -- it persists across lap rollovers until this sector completes again. */
  timeMs: number | null;
  /** This sector's time minus its previously-recorded time (lap-over-lap). null on the
   *  first recording (no baseline) and while the sector has never completed. */
  deltaMs: number | null;
  /** True when this sector is the session-fastest (the selected lap owns it, delta_to_best == 0).
   *  Only set on the selected-lap path (lapDtoToSectorTimes); undefined for the live timer. */
  fastest?: boolean;
};

export type SectorTimerInput = {
  /** Identifies the current session/lap-count context; changing it forces a reset even mid-lap. */
  sessionKey: string | null;
  lap: number;
  lapTimeNs: number;
  x: number;
  z: number;
};

type Slot = { timeMs: number | null; deltaMs: number | null };

export class SectorTimer {
  private readonly cl: ReadonlyArray<[number, number]>;
  private readonly arc: number[];
  private readonly partition: SectorPartition | null;

  private sessionKey: string | null = null;
  private lap: number | null = null;
  private currentIdx = 0;
  private entryLapTimeNs = 0;
  private lastLapTimeNs = 0;
  // Sticky per-sector display state. Survives lap rollover (only overwritten on a fresh
  // completion); cleared only on a session change or a brand-new timer.
  private displayed: Slot[];

  constructor(geometry: DrawGeometry | null) {
    this.cl = geometry?.cl ?? [];
    this.partition = geometry
      ? sectorBoundaries(geometry.cl, geometry.sectors.map(s => ({ sector: s.label, x: s.x, z: s.z })))
      : null;
    this.arc = this.partition ? clArcLengths(this.cl) : [];
    this.displayed = this.partition ? this.partition.order.map(() => ({ timeMs: null, deltaMs: null })) : [];
  }

  /** No geometry / fewer than 2 usable sector labels -> permanently null (caller keeps showing "pending"). */
  hasPartition(): boolean {
    return this.partition !== null;
  }

  /** Reset the running-lap cursor only. Displayed times are intentionally left intact. */
  private resetLap() {
    this.currentIdx = 0;
    this.entryLapTimeNs = 0;
  }

  /** Full reset (new session / new track): clear both the cursor and the sticky display. */
  private resetAll() {
    this.resetLap();
    this.displayed = this.partition ? this.partition.order.map(() => ({ timeMs: null, deltaMs: null })) : [];
  }

  /** Record a freshly-completed sector: overwrite its displayed time and compute the
   *  lap-over-lap delta against whatever it held before. A non-positive time is a bogus
   *  split (e.g. a rollover fired before any real accumulation) and is ignored, so it can
   *  never clobber a genuine (including a seeded/restored) value with 0. */
  private finalize(idx: number, timeMs: number) {
    if (idx < 0 || idx >= this.displayed.length) return;
    if (!(timeMs > 0)) return;
    const prev = this.displayed[idx].timeMs;
    this.displayed[idx] = { timeMs, deltaMs: prev !== null ? timeMs - prev : null };
  }

  /** Restore sticky display state after a page refresh. Seeds the session/lap context too so
   *  the first live update for the SAME session continues rather than resetting; a different
   *  incoming session still resets (its sessionKey won't match). */
  seedState(sessionKey: string | null, lap: number | null, times: SectorTime[]) {
    if (!this.partition) return;
    this.sessionKey = sessionKey;
    this.lap = lap;
    const order = this.partition.order;
    for (let i = 0; i < order.length && i < times.length; i++) {
      const t = times[i];
      if (!t) continue;
      const timeMs = typeof t.timeMs === "number" ? t.timeMs : null;
      const deltaMs = typeof t.deltaMs === "number" ? t.deltaMs : null;
      this.displayed[i] = { timeMs, deltaMs };
    }
  }

  update(input: SectorTimerInput): SectorTime[] | null {
    if (!this.partition) return null;

    if (input.sessionKey !== this.sessionKey) {
      this.sessionKey = input.sessionKey;
      this.lap = input.lap;
      this.lastLapTimeNs = 0;
      this.resetAll();
    } else if (this.lap !== null && input.lap !== this.lap) {
      // Lap rollover: finalize the sector we were in using the last known lap time
      // from just before the wrap (this frame's lapTimeNs has already reset near 0).
      this.finalize(this.currentIdx, (this.lastLapTimeNs - this.entryLapTimeNs) / 1_000_000);
      this.lap = input.lap;
      this.resetLap();
    } else if (this.lap === null) {
      this.lap = input.lap;
    }

    const arcLen = projectToClArc(this.cl, this.arc, input.x, input.z);
    if (!Number.isNaN(arcLen)) {
      const idx = sectorIndexAt(arcLen, this.partition.bounds);
      if (idx === this.currentIdx + 1) {
        this.finalize(this.currentIdx, (input.lapTimeNs - this.entryLapTimeNs) / 1_000_000);
        this.entryLapTimeNs = input.lapTimeNs;
        this.currentIdx = idx;
      }
    }

    this.lastLapTimeNs = input.lapTimeNs;

    return this.partition.order.map((sector, i) => ({
      sector,
      timeMs: this.displayed[i].timeMs,
      deltaMs: this.displayed[i].deltaMs,
    }));
  }
}
