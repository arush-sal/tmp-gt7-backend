import { describe, it, expect } from "vitest";
import { gateAfterRace } from "./gateAfterRace";
import type { LiveSource, LiveHandlers } from "./liveSource";
import type { LiveSnapshot } from "../contract/live";

const base: LiveSnapshot = {
  served_at: "2026-07-07T00:00:00.000Z", status: "live",
  frame: {
    car_speed: 214.3, current_gear: 4, suggested_gear: 5, rpm: 7200,
    throttle: 1, brake: 0, clutch: 0, boost: 1.2, current_fuel: 14.2, fuel_capacity: 60,
    tyre_temp_fl: 42.1, tyre_temp_fr: 30.6, tyre_temp_rl: 86.7, tyre_temp_rr: 80.0,
    current_lap: 8, total_laps: 10, current_position: 3, total_positions: 16,
    best_lap: "77.210s", last_lap: "78.004s", in_race: true, is_paused: false,
  },
  derived: { current_lap_time_ns: 77210000000, fuel_pct: 23.7, source: "LIVE" },
  enrichment: { session_id: "a", analysis: null, car_name: null, track: null, delta_to_best_ns: -910000000 },
};
const lap = (current_lap: number, total_laps = 10): LiveSnapshot => ({
  ...base, frame: { ...base.frame!, current_lap, total_laps },
});

function fakeSource() {
  let handlers: LiveHandlers | null = null;
  let closed = false;
  const source: LiveSource = (_url, h) => { handlers = h; return () => { closed = true; }; };
  return {
    source,
    emit: (s: LiveSnapshot) => handlers!.onSnapshot(s),
    isClosed: () => closed,
  };
}

describe("gateAfterRace", () => {
  it("forwards in-bounds frames unchanged", () => {
    const fake = fakeSource();
    const got: number[] = [];
    gateAfterRace(fake.source)("/x", { onSnapshot: s => got.push(s.frame!.current_lap), onError: () => {} });
    fake.emit(lap(8)); fake.emit(lap(9)); fake.emit(lap(10));
    expect(got).toEqual([8, 9, 10]);
  });

  it("forwards exactly ONE overflow frame (the finalizer), drops the cooldown spam", () => {
    const fake = fakeSource();
    const got: number[] = [];
    gateAfterRace(fake.source)("/x", { onSnapshot: s => got.push(s.frame!.current_lap), onError: () => {} });
    fake.emit(lap(10)); fake.emit(lap(11)); fake.emit(lap(11)); fake.emit(lap(11));
    expect(got).toEqual([10, 11]); // 10 in-bounds, 11 finalizer forwarded; further 11s dropped
  });

  it("unlatches when a new race brings current_lap back in bounds", () => {
    const fake = fakeSource();
    const got: number[] = [];
    gateAfterRace(fake.source)("/x", { onSnapshot: s => got.push(s.frame!.current_lap), onError: () => {} });
    fake.emit(lap(11)); fake.emit(lap(11)); // race over, latched
    fake.emit(lap(1));  fake.emit(lap(2));  // new race
    expect(got).toEqual([11, 1, 2]);
  });

  it("is a no-op for time trials (total_laps === 0)", () => {
    const fake = fakeSource();
    const got: number[] = [];
    gateAfterRace(fake.source)("/x", { onSnapshot: s => got.push(s.frame!.current_lap), onError: () => {} });
    fake.emit(lap(5, 0)); fake.emit(lap(6, 0)); fake.emit(lap(99, 0));
    expect(got).toEqual([5, 6, 99]);
  });

  it("passes through unsubscribe", () => {
    const fake = fakeSource();
    const unsub = gateAfterRace(fake.source)("/x", { onSnapshot: () => {}, onError: () => {} });
    unsub();
    expect(fake.isClosed()).toBe(true);
  });

  it("forwards onError unchanged", () => {
    let err: Error | null = null;
    let captured: LiveHandlers | null = null;
    const src: LiveSource = (_u, h) => { captured = h; return () => {}; };
    gateAfterRace(src)("/x", { onSnapshot: () => {}, onError: e => { err = e; } });
    captured!.onError(new Error("boom"));
    expect(err).toStrictEqual(new Error("boom"));
  });
});
