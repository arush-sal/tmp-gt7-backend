import { describe, it, expect } from "vitest";
import { createHistoryStore } from "./historyStore";
import type { LiveSnapshot } from "../contract/live";

const frame = (lap: number, lapTimeNs: number, throttle: number): LiveSnapshot => ({
  served_at: "2026-07-07T00:00:00Z", status: "live",
  frame: { car_speed: 100, current_gear: 3, suggested_gear: 3, rpm: 5000, throttle, brake: 0,
    clutch: 0, boost: 0, current_fuel: 50, fuel_capacity: 60,
    tyre_temp_fl: 80, tyre_temp_fr: 80, tyre_temp_rl: 80, tyre_temp_rr: 80,
    current_lap: lap, total_laps: 10, current_position: 1, total_positions: 2,
    best_lap: null, last_lap: null, in_race: true, is_paused: false },
  derived: { current_lap_time_ns: lapTimeNs, fuel_pct: 83, source: "LIVE" },
  enrichment: { analysis: null, car_name: null, track: null, delta_to_best_ns: 1_000_000 },
});
const idle: LiveSnapshot = { served_at: "x", status: "idle", frame: null, derived: null,
  enrichment: { analysis: null, car_name: null, track: null, delta_to_best_ns: null } };

describe("historyStore", () => {
  it("appends live frames and builds aligned columns (x = seconds)", () => {
    const s = createHistoryStore();
    s.push(frame(1, 1_000_000_000, 50)); // t=1.0s throttle 50
    s.push(frame(1, 2_000_000_000, 80)); // t=2.0s throttle 80
    const [xs, thr] = s.columns(["throttle"]);
    expect(xs).toEqual([1, 2]);
    expect(thr).toEqual([50, 80]);
    expect(s.size()).toBe(2);
  });
  it("ignores idle frames", () => {
    const s = createHistoryStore(); s.push(idle); expect(s.size()).toBe(0);
  });
  it("resets on lap increment", () => {
    const s = createHistoryStore();
    s.push(frame(1, 1_000_000_000, 50)); s.push(frame(2, 100_000_000, 60));
    expect(s.size()).toBe(1); // lap rollover cleared the buffer, kept only the new-lap sample
  });
  it("maps null delta to NaN (uPlot gap) not 0", () => {
    const s = createHistoryStore();
    const f = frame(1, 1e9, 50); (f.enrichment as any).delta_to_best_ns = null;
    s.push(f);
    const [, d] = s.columns(["delta"]);
    expect(Number.isNaN(d[0])).toBe(true);
  });
  it("drops a non-monotonic (regressing or duplicate) same-lap timestamp", () => {
    const s = createHistoryStore();
    s.push(frame(1, 2_000_000_000, 50)); // t=2.0s
    s.push(frame(1, 1_500_000_000, 60)); // t=1.5s regressed -> dropped
    s.push(frame(1, 2_000_000_000, 70)); // t=2.0s duplicate -> dropped
    expect(s.size()).toBe(1);
    expect(s.columns(["throttle"])[1]).toEqual([50]);
  });
});

// Minimal live snapshot builder. t = current_lap_time seconds; lap/total/session tunable.
function snap(opts: {
  t: number; lap: number; total?: number; sid?: string;
  throttle?: number; brake?: number; speed?: number;
  tfl?: number; tfr?: number; trl?: number; trr?: number; fuelPct?: number;
}): LiveSnapshot {
  const {
    t, lap, total = 3, sid = "s1",
    throttle = 0, brake = 0, speed = 0, tfl = 80, tfr = 81, trl = 82, trr = 83, fuelPct = 50,
  } = opts;
  return {
    served_at: "2026-07-08T00:00:00Z",
    status: "live",
    frame: {
      car_speed: speed, current_gear: 3, suggested_gear: 3, rpm: 5000,
      throttle, brake, clutch: 0, boost: 0, current_fuel: 40, fuel_capacity: 80,
      tyre_temp_fl: tfl, tyre_temp_fr: tfr, tyre_temp_rl: trl, tyre_temp_rr: trr,
      current_lap: lap, total_laps: total, current_position: 1, total_positions: 8,
      best_lap: null, last_lap: null, in_race: true, is_paused: false,
    },
    derived: { current_lap_time_ns: Math.round(t * 1e9), fuel_pct: fuelPct, source: "test" },
    enrichment: { analysis: null, car_name: null, track: null, delta_to_best_ns: null, session_id: sid },
  };
}

describe("historyStore — new channels", () => {
  it("projects tyre temps and fuel_pct", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, tfl: 90, tfr: 91, trl: 92, trr: 93, fuelPct: 77 }));
    const [, fl, fr, rl, rr, fuel] = s.columns(["tyreFL", "tyreFR", "tyreRL", "tyreRR", "fuel"]);
    expect([fl[0], fr[0], rl[0], rr[0], fuel[0]]).toEqual([90, 91, 92, 93, 77]);
  });
});

describe("historyStore — current-lap buffer + rollover", () => {
  it("clears the current buffer on rollover, keeping only the new lap's sample", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, speed: 100 }));
    s.push(snap({ t: 2, lap: 1, speed: 110 }));
    s.push(snap({ t: 1, lap: 2, speed: 200 })); // rollover -> current reset
    expect(s.size()).toBe(1); // only the lap-2 sample in the current buffer
  });

  it("does NOT self-reset on a session_id change (external reset() only)", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, sid: "a" }));
    s.push(snap({ t: 2, lap: 1, sid: "b" })); // session_id changed mid-lap
    expect(s.size()).toBe(2); // both retained
  });

  it("reset() clears everything (the controller-driven path)", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, sid: "a" }));
    s.push(snap({ t: 1, lap: 2, sid: "a" }));
    s.reset();
    expect(s.completedLapCount()).toBe(0);
    expect(s.size()).toBe(0);
  });

  it("push is a no-op while not live", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1 }));
    expect(s.size()).toBe(1);
    s.setLive(false);
    s.push(snap({ t: 2, lap: 1 }));
    expect(s.size()).toBe(1); // ignored while not live
    s.setLive(true);
    s.push(snap({ t: 3, lap: 1 }));
    expect(s.size()).toBe(2);
  });

  it("bounds the current-lap buffer to maxSamplesPerLap (drop-oldest)", () => {
    const s = createHistoryStore(undefined, 3);
    for (let i = 1; i <= 5; i++) s.push(snap({ t: i, lap: 1, speed: i * 10 }));
    expect(s.size()).toBe(3); // newest 3 kept
  });
});

describe("historyStore — onLapComplete + completedCount", () => {
  it("fires onLapComplete once per rollover with the finished buffer; completedCount increments", () => {
    const laps: { lap: number; n: number }[] = [];
    const s = createHistoryStore((lap, samples) => laps.push({ lap, n: samples.length }));
    s.push(snap({ t: 1, lap: 1 }));
    s.push(snap({ t: 2, lap: 1 }));
    expect(s.completedLapCount()).toBe(0);
    s.push(snap({ t: 0.5, lap: 2 })); // rollover -> lap 1 finished (2 samples)
    expect(laps).toEqual([{ lap: 1, n: 2 }]);
    expect(s.completedLapCount()).toBe(1);
    s.push(snap({ t: 1.5, lap: 2 }));
    s.push(snap({ t: 0.5, lap: 3 })); // rollover -> lap 2 finished
    expect(laps).toEqual([{ lap: 1, n: 2 }, { lap: 2, n: 2 }]);
    expect(s.completedLapCount()).toBe(2);
  });

  it("caps completedLapCount at total_laps (GT7 cooldown 'N+1/N' never inflates the count)", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, total: 2 }));
    s.push(snap({ t: 1, lap: 2, total: 2 })); // rollover lap 1 -> raw 1
    s.push(snap({ t: 1, lap: 3, total: 2 })); // rollover lap 2 -> raw 2
    s.push(snap({ t: 1, lap: 4, total: 2 })); // rollover lap 3 (cooldown) -> raw 3
    expect(s.completedLapCount()).toBe(2); // capped at total_laps
  });
});

function snapSpatial(lap: number, t: number, x: number, z: number, extra: Partial<any> = {}): LiveSnapshot {
  return {
    served_at: "t", status: "live",
    frame: { current_lap: lap, total_laps: 3, throttle: 50, brake: 0, clutch: 0, rpm: 1,
      current_gear: 3, suggested_gear: 3, car_speed: 100, boost: 0, current_fuel: 1, fuel_capacity: 1,
      tyre_temp_fl: 1, tyre_temp_fr: 1, tyre_temp_rl: 1, tyre_temp_rr: 1, current_position: 1,
      total_positions: 1, best_lap: null, last_lap: null, in_race: true, is_paused: false,
      position_x: x, position_z: z, surface_type: "TTTT", ...extra } as any,
    derived: { current_lap_time_ns: t * 1e9, fuel_pct: 50, source: "x" },
    enrichment: { session_id: "s1", analysis: null, car_name: null, track: null, delta_to_best_ns: null },
  };
}

test("trace() exposes only the current lap; a finished lap is handed to onLapComplete", () => {
  const completed: { lap: number; posX: number[] }[] = [];
  const h = createHistoryStore((lap, samples) => completed.push({ lap, posX: samples.map(s => s.posX) }));
  h.push(snapSpatial(1, 0.1, 10, 20));
  h.push(snapSpatial(1, 0.2, 11, 21));
  h.push(snapSpatial(2, 0.1, 12, 22, { surface_type: "TTCC" })); // lap rollover
  const tr = h.trace();
  expect(tr.current[0]).toMatchObject({ posX: 12, posZ: 22, surface: "TTCC" });
  expect(completed).toEqual([{ lap: 1, posX: [10, 11] }]);
});

test("revision() increments on push and lap rollover", () => {
  const h = createHistoryStore();
  const r0 = h.revision();
  h.push(snapSpatial(1, 0.1, 1, 1));
  const r1 = h.revision();
  h.push(snapSpatial(2, 0.1, 2, 2)); // rollover
  expect(r1).toBeGreaterThan(r0);
  expect(h.revision()).toBeGreaterThan(r1);
});

test("columns() still returns only channel data (no spatial leakage)", () => {
  const h = createHistoryStore();
  h.push(snapSpatial(1, 0.1, 9, 9));
  const cols = h.columns(["throttle"]);
  expect(cols.length).toBe(2);        // [xs, throttle]
  expect(cols[1]).toEqual([50]);
});

test("revision() does NOT bump on a rejected duplicate/out-of-order push", () => {
  const h = createHistoryStore();
  h.push(snapSpatial(1, 0.2, 1, 1));
  const r = h.revision();
  h.push(snapSpatial(1, 0.2, 2, 2)); // duplicate t -> rejected by strict-increasing guard
  h.push(snapSpatial(1, 0.1, 3, 3)); // earlier t -> rejected
  expect(h.revision()).toBe(r);
});

describe("historyStore — lastLapFuelUsedPct", () => {
  it("returns null when there is no completed lap", () => {
    const s = createHistoryStore();
    expect(s.lastLapFuelUsedPct()).toBeNull();
    s.push(snap({ t: 1, lap: 1, fuelPct: 90 }));
    expect(s.lastLapFuelUsedPct()).toBeNull(); // lap 1 still current, not completed
  });

  it("computes fuel used over the most recently completed lap", () => {
    const s = createHistoryStore();
    // lap 1: fuel 90 -> 80 (used 10)
    s.push(snap({ t: 0, lap: 1, fuelPct: 90 }));
    s.push(snap({ t: 1, lap: 1, fuelPct: 80 }));
    // rollover to lap 2: fuel 80 -> 65 (used 15)
    s.push(snap({ t: 0, lap: 2, fuelPct: 80 }));
    expect(s.lastLapFuelUsedPct()).toBe(10); // lap 1 just completed
    s.push(snap({ t: 1, lap: 2, fuelPct: 65 }));
    s.push(snap({ t: 0, lap: 3, fuelPct: 65 })); // rollover -> lap 2 completed
    expect(s.lastLapFuelUsedPct()).toBe(15);
  });

  it("keeps the last good value on a refuel/anomaly (non-positive delta)", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 0, lap: 1, fuelPct: 50 }));
    s.push(snap({ t: 1, lap: 1, fuelPct: 90 })); // refuelled mid-lap
    s.push(snap({ t: 0, lap: 2, fuelPct: 90 })); // rollover -> lap 1 completed (anomaly)
    expect(s.lastLapFuelUsedPct()).toBeNull(); // no prior good value -> stays null
  });
});

test("Channel excludes spatial fields (compile-time guard)", () => {
  const h = createHistoryStore();
  // @ts-expect-error posX is a spatial field, not a graphable Channel
  h.columns(["posX"]);
});
