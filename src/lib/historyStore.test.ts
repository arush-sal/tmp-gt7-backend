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

describe("historyStore — completed-lap retention", () => {
  it("pushes the finished lap onto completed laps on rollover and clears current", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, speed: 100 }));
    s.push(snap({ t: 2, lap: 1, speed: 110 }));
    s.push(snap({ t: 1, lap: 2, speed: 200 })); // rollover -> lap 1 retained, current reset
    expect(s.size()).toBe(1); // only the lap-2 sample in the current buffer
  });

  it("bounds retention to the last 20 completed laps", () => {
    const s = createHistoryStore();
    for (let lap = 1; lap <= 25; lap++) {
      s.push(snap({ t: 1, lap, speed: lap }));
      s.push(snap({ t: 2, lap, speed: lap }));
    }
    // 24 laps completed (lap 25 is current). Retention capped at 20.
    expect(s.completedLapCount()).toBe(20);
  });

  it("resets everything on session change", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, sid: "a" }));
    s.push(snap({ t: 1, lap: 2, sid: "a" }));
    s.push(snap({ t: 1, lap: 1, sid: "b" })); // new session
    expect(s.completedLapCount()).toBe(0);
    expect(s.size()).toBe(1);
  });

  it("retains the full current lap (no time-window eviction) so a long lap keeps early samples", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 0, lap: 1, speed: 100 }));
    s.push(snap({ t: 130, lap: 1, speed: 200 })); // lap spans >120s
    s.push(snap({ t: 0, lap: 2, speed: 100 }));
    s.push(snap({ t: 130, lap: 2, speed: 200 }));
    s.push(snap({ t: 10, lap: 3, speed: 999 })); // early x -> finite median (early samples retained)
    expect(Number.isFinite(s.medianColumn("speed")[0])).toBe(true);
  });

  it("bounds the current-lap buffer to maxSamplesPerLap (drop-oldest)", () => {
    const s = createHistoryStore(3);
    for (let i = 1; i <= 5; i++) s.push(snap({ t: i, lap: 1, speed: i * 10 }));
    expect(s.size()).toBe(3); // newest 3 kept
  });
});

describe("historyStore — medianColumn", () => {
  it("returns all-NaN with fewer than 2 completed laps", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, speed: 100 }));
    s.push(snap({ t: 1, lap: 2, speed: 120 })); // 1 completed lap
    const med = s.medianColumn("speed");
    expect(med.every(v => Number.isNaN(v))).toBe(true);
  });

  it("computes a per-point median across completed laps, interpolating by lap-time", () => {
    const s = createHistoryStore();
    // lap 1: (t=0,speed=100),(t=2,speed=200)
    s.push(snap({ t: 0, lap: 1, speed: 100 }));
    s.push(snap({ t: 2, lap: 1, speed: 200 }));
    // lap 2: (t=0,speed=200),(t=2,speed=300)
    s.push(snap({ t: 0, lap: 2, speed: 200 }));
    s.push(snap({ t: 2, lap: 2, speed: 300 }));
    // current lap 3 sampled at t=1 -> lap1 interp=150, lap2 interp=250, median=200
    s.push(snap({ t: 1, lap: 3, speed: 999 }));
    expect(s.medianColumn("speed")).toEqual([200]);
  });

  it("gaps (NaN) where the current x is beyond every completed lap's range", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 0, lap: 1, speed: 100 })); s.push(snap({ t: 1, lap: 1, speed: 100 }));
    s.push(snap({ t: 0, lap: 2, speed: 100 })); s.push(snap({ t: 1, lap: 2, speed: 100 }));
    s.push(snap({ t: 5, lap: 3, speed: 100 })); // beyond both laps' max t=1
    expect(s.medianColumn("speed")).toEqual([NaN]);
  });

  it("uses last 5 laps when total_laps > 20", () => {
    const s = createHistoryStore();
    // 6 completed laps of constant speed=lap#, total_laps=30 -> scope = last 5 (laps 2..6) -> median 4
    for (let lap = 1; lap <= 6; lap++) {
      s.push(snap({ t: 0, lap, total: 30, speed: lap }));
      s.push(snap({ t: 2, lap, total: 30, speed: lap }));
    }
    s.push(snap({ t: 1, lap: 7, total: 30, speed: 999 }));
    expect(s.medianColumn("speed")).toEqual([4]); // median of [2,3,4,5,6]
  });

  it("uses all laps when total_laps == 20 (<=20 boundary)", () => {
    const s = createHistoryStore();
    for (let lap = 1; lap <= 6; lap++) {
      s.push(snap({ t: 0, lap, total: 20, speed: lap }));
      s.push(snap({ t: 2, lap, total: 20, speed: lap }));
    }
    s.push(snap({ t: 1, lap: 7, total: 20, speed: 999 }));
    expect(s.medianColumn("speed")).toEqual([3.5]); // median of [1,2,3,4,5,6]
  });

  it("treats total_laps 0/null (unlimited) as the >20 rule (last 5)", () => {
    const s = createHistoryStore();
    for (let lap = 1; lap <= 6; lap++) {
      s.push(snap({ t: 0, lap, total: 0, speed: lap }));
      s.push(snap({ t: 2, lap, total: 0, speed: lap }));
    }
    s.push(snap({ t: 1, lap: 7, total: 0, speed: 999 }));
    expect(s.medianColumn("speed")).toEqual([4]);
  });

  it("handles a degenerate single-sample completed lap without dividing by zero", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, speed: 100 })); // single-sample lap 1
    s.push(snap({ t: 0, lap: 2, speed: 200 })); s.push(snap({ t: 2, lap: 2, speed: 300 }));
    s.push(snap({ t: 1, lap: 3, speed: 999 }));
    // lap1 has only t=1 -> interp at t=1 = 100; lap2 interp at t=1 = 250; median = 175
    expect(s.medianColumn("speed")).toEqual([175]);
  });
});
