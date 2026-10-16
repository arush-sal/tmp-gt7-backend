import { describe, it, expect } from "vitest";
import { lapViewFromSamples, lapViewFromTrace, lapDtoToSectorTimes, type LapSample } from "./lapView";
import type { TraceSampleDTO, LapDTO } from "../contract/session";

const dto = (over: Partial<TraceSampleDTO> = {}): TraceSampleDTO => ({
  seq: 0, t_ns: 1_000_000_000, distance: 10, pos_x: 100, pos_z: 200,
  throttle: 80, brake: 0, speed: 150, gear: 4, rpm: 8000,
  fuel: 55, tyre_temp_fl: 78, tyre_temp_fr: 79, tyre_temp_rl: 86, tyre_temp_rr: 85,
  clutch: 1, boost: 0.5, delta_to_best_ns: -250_000_000, surface_type: "TTGT", ...over,
});

describe("lapViewFromTrace", () => {
  const v = lapViewFromTrace([dto(), dto({ seq: 1, t_ns: 2_000_000_000, pos_x: 110, delta_to_best_ns: null })]);
  it("remaps pos_x/pos_z/t_ns; surface from surface_type (D-B30)", () => {
    expect(v.points[0].posX).toBe(100);
    expect(v.points[0].posZ).toBe(200);
    expect(v.points[0].t).toBe(1); // t_ns / 1e9
    expect(v.points[0].surface).toBe("TTGT");
    expect(v.length).toBe(2);
  });
  it("columns aligns xs (lap-time) + channels; null delta -> NaN", () => {
    const [xs, thr, tyreFL, delta] = v.columns(["throttle", "tyreFL", "delta"]);
    expect(xs).toEqual([1, 2]);
    expect(thr).toEqual([80, 80]);
    expect(tyreFL).toEqual([78, 78]);
    expect(delta[0]).toBeCloseTo(-0.25); // delta_to_best_ns / 1e9 seconds
    expect(Number.isNaN(delta[1])).toBe(true);
  });
  it("frameAt returns FrameValues; out-of-range -> null", () => {
    const f = v.frameAt(0)!;
    expect(f.fuel).toBe(55);
    expect(f.tyreRR).toBe(85);
    expect(f.speed).toBe(150);
    expect(v.frameAt(99)).toBeNull();
  });
});

describe("lapViewFromSamples", () => {
  const s: LapSample = {
    t: 1, posX: 1, posZ: 2, surface: "TTTT", distance: 0, throttle: 50, brake: 0, clutch: 1, rpm: 5000,
    gear: 3, speed: 100, boost: 0, delta: -0.1, tyreFL: 80, tyreFR: 81, tyreRL: 82, tyreRR: 83, fuel: 40,
  };
  const v = lapViewFromSamples([s]);
  it("reads samples directly; preserves surface (in-session)", () => {
    expect(v.columns(["speed"])).toEqual([[1], [100]]);
    expect(v.frameAt(0)!.fuel).toBe(40);
    expect(v.points[0].surface).toBe("TTTT");
  });
});

describe("lapDtoToSectorTimes", () => {
  const lap: LapDTO = {
    lap_number: 3, duration_ns: 90_000_000_000, complete: true, valid: true, timed: true,
    delta_to_pb_ns: 500_000_000,
    sectors: { reliable: true, reason: null, splits: [
      { index: 0, label: "S1", duration_ns: 30_000_000_000, delta_to_best_ns: 100_000_000, min_speed: 90 },
      { index: 1, label: "S2", duration_ns: 60_000_000_000, delta_to_best_ns: null, min_speed: 120 },
      { index: 2, label: "S3", duration_ns: 25_000_000_000, delta_to_best_ns: 0, min_speed: 110 },
    ] },
  };
  it("maps label->sector, ns->ms, null delta->null; fastest when delta_to_best==0", () => {
    expect(lapDtoToSectorTimes(lap)).toEqual([
      { sector: "S1", timeMs: 30_000, deltaMs: 100, fastest: false },
      { sector: "S2", timeMs: 60_000, deltaMs: null, fastest: false },
      { sector: "S3", timeMs: 25_000, deltaMs: 0, fastest: true },
    ]);
  });
});
