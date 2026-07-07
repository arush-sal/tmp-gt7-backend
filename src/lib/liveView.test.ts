import { describe, it, expect } from "vitest";
import { toLiveView, isStale } from "./liveView";
import type { LiveSnapshot } from "../contract/live";

const base: LiveSnapshot = {
  served_at: "2026-07-07T00:00:00.000Z", status: "live",
  frame: {
    car_speed: 214.3, current_gear: 4, suggested_gear: 5, rpm: 7200,
    throttle: 1, brake: 0, clutch: 0, boost: 1.2, current_fuel: 14.2, fuel_capacity: 60,
    tyre_temp_fl: 88, tyre_temp_fr: 96, tyre_temp_rl: 82, tyre_temp_rr: 90,
    current_lap: 8, total_laps: 20, current_position: 3, total_positions: 16,
    best_lap: "77.210s", last_lap: "78.004s", in_race: true, is_paused: false,
  },
  derived: { current_lap_time_ns: 77210000000, fuel_pct: 23.7, source: "LIVE" },
  enrichment: { analysis: null, car_name: null, track: null, delta_to_best_ns: -910000000 },
};

describe("toLiveView (live)", () => {
  const v = toLiveView(base);
  it("formats lap counter", () => expect(v.lap).toBe("8/20"));
  it("formats position", () => expect(v.position).toBe("P3/16"));
  it("formats best lap from Duration", () => expect(v.bestLap).toBe("1:17.210"));
  it("formats delta from ns", () => expect(v.delta).toBe("−0.910s"));
  it("formats fuel pct", () => expect(v.fuelPct).toBe("24%"));
  it("formats a tyre temp", () => expect(v.tyreTemps.fr).toBe("96°C"));
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
    for (const val of [v.speed, v.gear, v.rpm, v.lap, v.position, v.fuelPct, v.currentLapTime, v.delta, v.bestLap, v.lastLap, v.tyreTemps.fl, v.tyreTemps.fr, v.tyreTemps.rl, v.tyreTemps.rr]) {
      expect(val).toBe("—");
    }
  });
});

describe("isStale", () => {
  const t0 = Date.parse("2026-07-07T00:00:00.000Z");
  it("fresh within threshold", () => expect(isStale(base, t0 + 1000)).toBe(false));
  it("stale beyond threshold", () => expect(isStale(base, t0 + 5000)).toBe(true));
  it("treats an unparseable served_at as stale", () =>
    expect(isStale({ ...base, served_at: "not-a-date" }, 0)).toBe(true));
});
