import { describe, it, expect } from "vitest";
import { toLiveView, isStale, frameToDisplay } from "./liveView";
import { PLACEHOLDER } from "./format";
import type { LiveSnapshot } from "../contract/live";
import type { FrameValues } from "./historyStore";

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
  it("caps displayed lap at total_laps (post-race cooldown)", () => {
    const capped = toLiveView({ ...base, frame: { ...base.frame!, current_lap: 11, total_laps: 10 } });
    expect(capped.lap).toBe("10/10");
  });
  it("leaves lap uncapped when total_laps is 0 (time trial)", () => {
    const uncapped = toLiveView({ ...base, frame: { ...base.frame!, current_lap: 3, total_laps: 0 } });
    expect(uncapped.lap).toBe("3");
  });
  it("placeholders lap when current_lap < 0 (menus)", () => {
    expect(toLiveView({ ...base, frame: { ...base.frame!, current_lap: -1, total_laps: 0 } }).lap).toBe(PLACEHOLDER);
  });
  it("placeholders position when GT7 reports -1 (time trial / menus)", () => {
    expect(toLiveView({ ...base, frame: { ...base.frame!, current_position: -1, total_positions: -1 } }).position).toBe(PLACEHOLDER);
    expect(toLiveView({ ...base, frame: { ...base.frame!, current_position: 0, total_positions: 8 } }).position).toBe(PLACEHOLDER);
  });
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

  it("tyre tone: under < 75, optimal 75..90, over > 90", () => {
    const v = toLiveView(base); // rl 86.8 optimal, rr 80.1 optimal, fl 42.2 under, fr 30.7 under
    expect(v.tyreTemps.rl.tone).toBe("optimal");
    expect(v.tyreTemps.rr.tone).toBe("optimal");
    expect(v.tyreTemps.fl.tone).toBe("under");
    expect(v.tyreTemps.rl.value).toBe("87°C");
  });
  it("tyre tone none + placeholder when idle", () => {
    const v = toLiveView({ ...base, status: "idle", frame: null, derived: null });
    expect(v.tyreTemps.fl).toEqual({ value: "—", tone: "none" });
  });

  it("delta tone is none at exactly 0", () =>
    expect(toLiveView({ ...base, enrichment: { ...base.enrichment, delta_to_best_ns: 0 } }).deltaTone).toBe("none"));
  it("tyre tone boundaries: 75 and 90 are optimal; just outside flips", () => {
    expect(toLiveView(withTyreFl(75)).tyreTemps.fl.tone).toBe("optimal");
    expect(toLiveView(withTyreFl(90)).tyreTemps.fl.tone).toBe("optimal");
    expect(toLiveView(withTyreFl(74.9)).tyreTemps.fl.tone).toBe("under");
    expect(toLiveView(withTyreFl(90.1)).tyreTemps.fl.tone).toBe("over");
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

describe("frameToDisplay", () => {
  const fr: FrameValues = {
    t: 77.21, fuel: 23.7,
    tyreFL: 42.15736, tyreFR: 30.689157, tyreRL: 86.77724, tyreRR: 80.08314,
    delta: -0.91, speed: 214.3, gear: 4, rpm: 7200, throttle: 1, brake: 0,
  };
  it("formats a stored frame into display fields", () => {
    const d = frameToDisplay(fr);
    expect(d.currentLapTime).toBe("1:17.210");
    expect(d.fuelPct).toBe("24%");
    expect(d.fuelTone).toBe("red");
    expect(d.tyreTemps.rl.value).toBe("87°C");
    expect(d.delta).toBe("−0.910s");
    expect(d.deltaTone).toBe("blue");
  });
  it("placeholders a NaN delta", () => {
    const d = frameToDisplay({ ...fr, delta: NaN });
    expect(d.delta).toBe("—");
    expect(d.deltaTone).toBe("none");
  });
});

describe("isStale", () => {
  const t0 = Date.parse("2026-07-07T00:00:00.000Z");
  it("fresh within threshold", () => expect(isStale(base, t0 + 1000)).toBe(false));
  it("stale beyond threshold", () => expect(isStale(base, t0 + 5000)).toBe(true));
  it("treats an unparseable served_at as stale", () =>
    expect(isStale({ ...base, served_at: "not-a-date" }, 0)).toBe(true));
});

describe("toLiveView predicted", () => {
  it("formats predicted-best from analysis.predicted_best_ns", () => {
    const v = toLiveView({ ...base, enrichment: { ...base.enrichment, analysis: { predicted_best_ns: 115_100_000_000 } } });
    expect(v.predicted).toBe("1:55.100");
  });
  it("placeholder when analysis null or predicted 0", () => {
    expect(toLiveView(base).predicted).toBe("—"); // base has analysis: null
    expect(toLiveView({ ...base, enrichment: { ...base.enrichment, analysis: { predicted_best_ns: 0 } } }).predicted).toBe("—");
  });
});
