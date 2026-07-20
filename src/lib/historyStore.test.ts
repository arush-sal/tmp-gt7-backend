import { describe, it, expect } from "vitest";
import { createHistoryStore, PERSIST_TARGET_SAMPLES, PERSIST_MAX_LAPS } from "./historyStore";
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

test("trace() exposes posX/posZ/surface across completed + current laps", () => {
  const h = createHistoryStore();
  h.push(snapSpatial(1, 0.1, 10, 20));
  h.push(snapSpatial(1, 0.2, 11, 21));
  h.push(snapSpatial(2, 0.1, 12, 22, { surface_type: "TTCC" })); // lap rollover
  const tr = h.trace();
  expect(tr.completed.length).toBe(1);
  expect(tr.completed[0].map(p => p.posX)).toEqual([10, 11]);
  expect(tr.current[0]).toMatchObject({ posX: 12, posZ: 22, surface: "TTCC" });
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

  it("returns null on a refuel/anomaly (non-positive delta)", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 0, lap: 1, fuelPct: 50 }));
    s.push(snap({ t: 1, lap: 1, fuelPct: 90 })); // refuelled mid-lap
    s.push(snap({ t: 0, lap: 2, fuelPct: 90 })); // rollover -> lap 1 completed
    expect(s.lastLapFuelUsedPct()).toBeNull();
  });
});

test("Channel excludes spatial fields (compile-time guard)", () => {
  const h = createHistoryStore();
  // @ts-expect-error posX is a spatial field, not a graphable Channel
  h.columns(["posX"]);
});

describe("historyStore — lapColumns / lapSampleCount", () => {
  it("returns [xs, ...channels] for a completed lap, same shape as columns()", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1, throttle: 50 }));
    s.push(snap({ t: 2, lap: 1, throttle: 80 }));
    s.push(snap({ t: 0.5, lap: 2, throttle: 10 })); // rollover -> lap 1 completed
    const cols = s.lapColumns(0, ["throttle"]);
    expect(cols).not.toBeNull();
    const [xs, thr] = cols!;
    expect(xs).toEqual([1, 2]);
    expect(thr).toEqual([50, 80]);
  });

  it("returns null when the lap index is out of range", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1 }));
    expect(s.lapColumns(0, ["throttle"])).toBeNull();
    expect(s.lapColumns(-1, ["throttle"])).toBeNull();
  });

  it("lapSampleCount returns the completed lap's sample count, 0 if out of range", () => {
    const s = createHistoryStore();
    s.push(snap({ t: 1, lap: 1 }));
    s.push(snap({ t: 2, lap: 1 }));
    s.push(snap({ t: 0.5, lap: 2 })); // rollover -> lap 1 completed with 2 samples
    expect(s.lapSampleCount(0)).toBe(2);
    expect(s.lapSampleCount(1)).toBe(0);
    expect(s.lapSampleCount(-1)).toBe(0);
  });
});

describe("historyStore — persistence (dump/load)", () => {
  it("dump() returns null for an empty store", () => {
    expect(createHistoryStore().dump()).toBeNull();
  });

  it("round-trips completed laps + the running buffer into a fresh store", () => {
    const a = createHistoryStore();
    a.push(frame(1, 1e9, 40));
    a.push(frame(1, 2e9, 60));
    a.push(frame(2, 0.5e9, 80)); // rollover: lap 1 completed (2 samples), lap 2 running (1)
    const data = a.dump()!;
    expect(data).not.toBeNull();

    const b = createHistoryStore();
    b.load(data);
    expect(b.completedLapCount()).toBe(1);
    expect(b.size()).toBe(1);                                 // running lap 2 has 1 sample
    expect(b.lapColumns(0, ["throttle"])![1]).toEqual([40, 60]); // lap 1 restored
    expect(b.columns(["throttle"])[1]).toEqual([80]);         // running lap restored
  });

  it("thins a huge lap to the sample cap, preserving first + last", () => {
    const a = createHistoryStore();
    for (let i = 0; i < 5000; i++) a.push(frame(1, (i + 1) * 1e6, i % 100));
    const data = a.dump()!;
    expect(data.buf.length).toBeLessThanOrEqual(PERSIST_TARGET_SAMPLES + 1);
    expect(data.buf[0].throttle).toBe(0);                     // first
    expect(data.buf[data.buf.length - 1].throttle).toBe(4999 % 100); // last preserved
  });

  it("persists only the most recent laps (cap)", () => {
    const a = createHistoryStore();
    for (let lap = 1; lap <= 12; lap++) {
      a.push(frame(lap, 1e9, 50));
      a.push(frame(lap, 2e9, 50));
    }
    expect(a.dump()!.completed.length).toBe(PERSIST_MAX_LAPS);
  });

  it("load(dump()) then a push does not clear the loaded laps", () => {
    const a = createHistoryStore();
    a.push(frame(1, 1e9, 40));
    a.push(frame(1, 2e9, 60));
    a.push(frame(2, 0.5e9, 80)); // rollover -> 1 completed lap
    const data = a.dump();

    const b = createHistoryStore();
    b.load(data);
    expect(b.completedLapCount()).toBe(1);
    b.push(frame(2, 1e9, 90)); // same lap, later t -> appended, not a reset
    expect(b.completedLapCount()).toBe(1);
    expect(b.size()).toBe(2);
  });

  it("ignores a malformed / wrong-version payload", () => {
    const b = createHistoryStore();
    b.load({ v: 2, completed: [], buf: [] });
    b.load(null);
    b.load("garbage");
    expect(b.dump()).toBeNull();
  });
});
