// src/lib/lapView.ts
// A read-only view over one lap's samples, exposing the same accessors the consumers used to call on
// historyStore (points / columns / frameAt). Two builders feed it: `lapViewFromSamples` for the live
// in-session cache (full-fidelity Sample rows), and `lapViewFromTrace` for a backend-served completed
// lap (the single snake_case -> camelCase remap point). This lets TrackTraceCanvas / TelemetryGraph /
// the scrub read a fetched lap without reaching into the imperative live store.
import type { Channel, TracePoint, FrameValues } from "./historyStore";
import type { TraceSampleDTO, LapDTO } from "../contract/session";
import type { SectorTime } from "./sectorTimer";

// A full-channel lap row: structurally identical to historyStore's internal Sample, so the live
// cache can hand its Samples straight to `lapViewFromSamples`.
export type LapSample = { t: number; posX: number; posZ: number; surface: string } & Record<Channel, number>;

export type LapView = {
  readonly points: readonly TracePoint[];
  readonly length: number;
  /** [xs, ...perChannel] where xs is lap-time seconds — same shape as historyStore.lapColumns. */
  columns(chs: Channel[]): number[][];
  /** FrameValues at sample i (same shape as historyStore.frameAt); null out of range. */
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

/** Backend-served completed lap. The single place snake_case wire fields become camelCase samples. */
export const lapViewFromTrace = (dtos: TraceSampleDTO[]): LapView => make(dtos.map(dtoToSample));

function dtoToSample(d: TraceSampleDTO): LapSample {
  return {
    t: d.t_ns / 1e9, posX: d.pos_x, posZ: d.pos_z, surface: "", // DTO carries no surface (kerb/grass ticks unavailable)
    throttle: d.throttle, brake: d.brake, clutch: d.clutch, rpm: d.rpm,
    gear: d.gear, speed: d.speed, boost: d.boost,
    delta: d.delta_to_best_ns == null ? NaN : d.delta_to_best_ns / 1e9,
    tyreFL: d.tyre_temp_fl, tyreFR: d.tyre_temp_fr, tyreRL: d.tyre_temp_rl, tyreRR: d.tyre_temp_rr,
    fuel: d.fuel,
  };
}

/** Adapt a lap's backend sector splits to the RightPanel's SectorTime[]. deltaMs is vs session best
 *  (backend semantics), not lap-over-lap. */
export function lapDtoToSectorTimes(lap: LapDTO): SectorTime[] {
  return lap.sectors.splits.map(s => ({
    sector: s.label,
    timeMs: s.duration_ns / 1e6,
    deltaMs: s.delta_to_best_ns == null ? null : s.delta_to_best_ns / 1e6,
  }));
}
