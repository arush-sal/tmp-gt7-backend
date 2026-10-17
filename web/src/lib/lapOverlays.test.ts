import { describe, it, expect } from "vitest";
import { resampleByFraction, lapViewFromTrace } from "./lapView";
import { overlayColumns } from "./lapOverlays";
import type { TraceSampleDTO, MedianDTO, ReferenceDTO, PredictedSampleDTO } from "../contract/session";

describe("resampleByFraction", () => {
  const grid = [0, 0.5, 1];
  const values = [0, 10, 20];
  it("interpolates linearly between grid points", () => {
    expect(resampleByFraction(grid, values, [0.25, 0.5, 0.75])).toEqual([5, 10, 15]);
  });
  it("clamps targets outside the grid to the endpoints", () => {
    expect(resampleByFraction(grid, values, [-1, 2])).toEqual([0, 20]);
  });
  it("handles non-monotonic targets (walks the cursor back)", () => {
    expect(resampleByFraction(grid, values, [0.75, 0.25])).toEqual([15, 5]);
  });
  it("empty grid -> NaN per target (nothing to draw)", () => {
    expect(resampleByFraction([], [], [0.5])).toEqual([NaN]);
  });
});

// A 3-sample lap at track distances 0/500/1000m -> fractions 0/0.5/1.
const trace = (i: number, dist: number): TraceSampleDTO => ({
  seq: i, t_ns: i * 1e9, distance: dist, pos_x: 0, pos_z: 0, throttle: 0, brake: 0, speed: 0,
  gear: 0, rpm: 0, fuel: 0, tyre_temp_fl: 0, tyre_temp_fr: 0, tyre_temp_rl: 0, tyre_temp_rr: 0,
  clutch: 0, boost: 0, delta_to_best_ns: null, surface_type: "", steering: 0,
});
const lap = lapViewFromTrace([trace(0, 0), trace(1, 500), trace(2, 1000)]);

describe("overlayColumns", () => {
  it("null lap -> both null", () => {
    expect(overlayColumns(null, "speed", null, null)).toEqual({ median: null, reference: null });
  });

  it("resamples the median series onto the lap's fractions", () => {
    const median: MedianDTO = { fractions: [0, 1], series: { speed: [100, 200] }, lap_count: 3 };
    expect(overlayColumns(lap, "speed", median, null).median).toEqual([100, 150, 200]);
  });

  it("no median for a channel the backend doesn't median -> null", () => {
    const median: MedianDTO = { fractions: [0, 1], series: { speed: [100, 200] }, lap_count: 3 };
    expect(overlayColumns(lap, "fuel", median, null).median).toBeNull();
  });

  it("resamples the reference channel onto the lap's fractions", () => {
    const rs = (fraction: number, speed: number): PredictedSampleDTO => ({
      fraction, t_ns: 0, distance: 0, pos_x: 0, pos_z: 0, speed, throttle: 0, brake: 0, rpm: 0,
      gear: 0, fuel: 0, tyre_temp_fl: 0, tyre_temp_fr: 0, tyre_temp_rl: 0, tyre_temp_rr: 0,
      clutch: 0, boost: 0, delta_to_best_ns: null, steering: 0,
    });
    const reference: ReferenceDTO = {
      reliable: true, reason: null, predicted_best_ns: 0, sectors: [], samples: [rs(0, 300), rs(1, 100)],
    };
    expect(overlayColumns(lap, "speed", null, reference).reference).toEqual([300, 200, 100]);
  });

  it("steering reference is converted from the DTO's radians to degrees", () => {
    const rs = (fraction: number, steering: number): PredictedSampleDTO => ({
      fraction, t_ns: 0, distance: 0, pos_x: 0, pos_z: 0, speed: 0, throttle: 0, brake: 0, rpm: 0,
      gear: 0, fuel: 0, tyre_temp_fl: 0, tyre_temp_fr: 0, tyre_temp_rl: 0, tyre_temp_rr: 0,
      clutch: 0, boost: 0, delta_to_best_ns: null, steering,
    });
    const reference: ReferenceDTO = {
      reliable: true, reason: null, predicted_best_ns: 0, sectors: [], samples: [rs(0, Math.PI / 18), rs(1, -Math.PI / 18)],
    };
    const col = overlayColumns(lap, "steering", null, reference).reference!;
    expect(col.map(v => Math.round(v * 1e6) / 1e6)).toEqual([10, 0, -10]);
  });

  it("tyre channel gets no reference (excluded)", () => {
    const reference: ReferenceDTO = { reliable: true, reason: null, predicted_best_ns: 0, sectors: [], samples: [] };
    expect(overlayColumns(lap, "tyreFL", null, reference).reference).toBeNull();
  });
});

describe("overlayColumns fed a stored-lap DTO", () => {
  it("overlays the stored lap's channels (steering rad->deg) on the lap's fractions", async () => {
    const { storedRef } = await import("../mock/storedRef");
    const ref = storedRef();
    expect(overlayColumns(lap, "speed", null, ref).reference).toEqual([100, 150, 200]);
    const steer = overlayColumns(lap, "steering", null, ref).reference!;
    expect(steer[0]).toBeCloseTo(0);
    expect(steer[1]).toBeCloseTo(180); // PI rad
  });
});

describe("sectorsVsReference", () => {
  const times = [{ sector: "S1", timeMs: 31000, deltaMs: 5, fastest: true }];
  it("deltas vs the reference's sector by label", async () => {
    const { sectorsVsReference } = await import("./lapView");
    const { storedRef } = await import("../mock/storedRef");
    expect(sectorsVsReference(times, storedRef())[0]).toMatchObject({ deltaMs: 1000, fastest: false });
  });
  it("no delta when the lap's sectors or the reference are unreliable", async () => {
    const { sectorsVsReference } = await import("./lapView");
    const { storedRef } = await import("../mock/storedRef");
    expect(sectorsVsReference(times, storedRef(), false)[0].deltaMs).toBeNull();
    expect(sectorsVsReference(times, storedRef({ reliable: false }))[0].deltaMs).toBeNull();
  });
});
