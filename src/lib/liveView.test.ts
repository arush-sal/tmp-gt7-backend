import { describe, it, expect } from "vitest";
import { toLiveView, isStale } from "./liveView";
import type { LiveSnapshot } from "../contract/live";

const base: LiveSnapshot = {
  served_at: "2026-07-07T00:00:00.000Z", status: "live",
  frame: {
    car_speed: 214.3, current_gear: 4, suggested_gear: 5, rpm: 7200,
    throttle: 1, brake: 0, clutch: 0, boost: 1.2, current_fuel: 14.2, fuel_capacity: 60,
    tyre_temp_fl: 42.15736, tyre_temp_fr: 30.689157, tyre_temp_rl: 86.77724, tyre_temp_rr: 80.08314,
    current_lap: 8, total_laps: 20, current_position: 3, total_positions: 16,
    best_lap: "77.210s", last_lap: "78.004s", in_race: true, is_paused: false,
  },
  derived: { current_lap_time_ns: 77210000000, fuel_pct: 23.7, source: "LIVE" },
  enrichment: { analysis: null, car_name: null, track: null, delta_to_best_ns: -910000000 },
};

const withFuel = (pct: number) => ({ ...base, derived: { ...base.derived!, fuel_pct: pct } });
const withTyreFl = (c: number) => ({ ...base, frame: { ...base.frame!, tyre_temp_fl: c } });

describe("toLiveView (live)", () => {
  const v = toLiveView(base);
  it("formats lap counter", () => expect(v.lap).toBe("8/20"));
  it("formats position", () => expect(v.position).toBe("P3/16"));
  it("formats best lap from Duration", () => expect(v.bestLap).toBe("1:17.210"));
  it("formats delta from ns", () => expect(v.delta).toBe("−0.910s"));
  it("formats fuel pct", () => expect(v.fuelPct).toBe("24%"));
  it("formats a tyre temp", () => expect(v.tyreTemps.fr.value).toBe("31°C"));
});

describe("toLiveView (nulls / idle)", () => {
  it("placeholders a null delta", () => {
    const v = toLiveView({ ...base, enrichment: { ...base.enrichment, delta_to_best_ns: null } });
    expect(v.delta).toBe("—");
  });
  it("placeholders a null best_lap", () => {
    const v = toLiveView({ ...base, frame: { ...base.frame!, best_lap: null } });
    expect(v.bestLap).toBe("—");
  });
  it("placeholders every live field when idle (incl. a lingering delta)", () => {
    const v = toLiveView({ ...base, status: "idle", frame: null, derived: null, enrichment: { ...base.enrichment, delta_to_best_ns: -910000000 } });
    expect(v.status).toBe("idle");
    for (const val of [v.speed, v.gear, v.rpm, v.lap, v.position, v.fuelPct, v.currentLapTime, v.delta, v.bestLap, v.lastLap, v.tyreTemps.fl.value, v.tyreTemps.fr.value, v.tyreTemps.rl.value, v.tyreTemps.rr.value]) {
      expect(val).toBe("—");
    }
  });
});

describe("toLiveView tones", () => {
  it("delta tone is blue when ahead (negative)", () =>
    expect(toLiveView({ ...base, enrichment: { ...base.enrichment, delta_to_best_ns: -5 } }).deltaTone).toBe("blue"));
  it("delta tone is red when behind (positive)", () =>
    expect(toLiveView({ ...base, enrichment: { ...base.enrichment, delta_to_best_ns: 5 } }).deltaTone).toBe("red"));
  it("delta tone is none when null", () =>
    expect(toLiveView({ ...base, enrichment: { ...base.enrichment, delta_to_best_ns: null } }).deltaTone).toBe("none"));
  it("delta tone is none when idle", () =>
    expect(toLiveView({ ...base, status: "idle", frame: null, derived: null }).deltaTone).toBe("none"));

  it("fuel tone green above 85", () =>
    expect(toLiveView(withFuel(90)).fuelTone).toBe("green"));
  it("fuel tone yellow in 25..85 inclusive", () => {
    expect(toLiveView(withFuel(85)).fuelTone).toBe("yellow");
    expect(toLiveView(withFuel(25)).fuelTone).toBe("yellow");
  });
  it("fuel tone red below 25", () =>
    expect(toLiveView(withFuel(24)).fuelTone).toBe("red"));
  it("fuel tone none when idle", () =>
    expect(toLiveView({ ...base, status: "idle", frame: null, derived: null }).fuelTone).toBe("none"));

  it("tyre tone hot above 85, warm 55..85, cold below 55", () => {
    const v = toLiveView(base); // rl 86.8 hot, rr 80.1 warm, fl 42.2 cold, fr 30.7 cold
    expect(v.tyreTemps.rl.tone).toBe("hot");
    expect(v.tyreTemps.rr.tone).toBe("warm");
    expect(v.tyreTemps.fl.tone).toBe("cold");
    expect(v.tyreTemps.rl.value).toBe("87°C");
  });
  it("tyre tone none + placeholder when idle", () => {
    const v = toLiveView({ ...base, status: "idle", frame: null, derived: null });
    expect(v.tyreTemps.fl).toEqual({ value: "—", tone: "none" });
  });

  it("delta tone is none at exactly 0", () =>
    expect(toLiveView({ ...base, enrichment: { ...base.enrichment, delta_to_best_ns: 0 } }).deltaTone).toBe("none"));
  it("tyre tone boundaries: 85 and 55 are warm; just outside flips", () => {
    expect(toLiveView(withTyreFl(85)).tyreTemps.fl.tone).toBe("warm");
    expect(toLiveView(withTyreFl(55)).tyreTemps.fl.tone).toBe("warm");
    expect(toLiveView(withTyreFl(85.1)).tyreTemps.fl.tone).toBe("hot");
    expect(toLiveView(withTyreFl(54.9)).tyreTemps.fl.tone).toBe("cold");
  });
});

describe("toLiveView — format-C CONFIRMED fields", () => {
  const withC = (): LiveSnapshot => ({
    ...base,
    frame: {
      ...base.frame!,
      wheel_steering_angle_front_left: 0.1, wheel_steering_angle_front_right: 0.14,
      surface_type: "TTTT", car_category: "GRN", wheel_base: 2.6,
    },
  });
  it("exposes averaged steering angle in degrees when present", () => {
    // mean(0.1, 0.14) = 0.12 rad -> 6.9°
    expect(toLiveView(withC()).steering).toBe("+6.9°");
  });
  it("exposes surface and car category when present", () => {
    const v = toLiveView(withC());
    expect(v.surface).toBe("TTTT");
    expect(v.carCategory).toBe("GRN");
  });
  it("placeholders format-C fields when absent (format-A / older source)", () => {
    const v = toLiveView(base);
    expect(v.steering).toBe("—");
    expect(v.surface).toBe("—");
    expect(v.carCategory).toBe("—");
  });
});

describe("isStale", () => {
  const t0 = Date.parse("2026-07-07T00:00:00.000Z");
  it("fresh within threshold", () => expect(isStale(base, t0 + 1000)).toBe(false));
  it("stale beyond threshold", () => expect(isStale(base, t0 + 5000)).toBe(true));
  it("treats an unparseable served_at as stale", () =>
    expect(isStale({ ...base, served_at: "not-a-date" }, 0)).toBe(true));
});
