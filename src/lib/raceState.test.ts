import { describe, it, expect } from "vitest";
import { deriveLive, stepRace, initialRaceKey } from "./raceState";
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
  enrichment: { session_id: "a", analysis: null, car_name: null, track: null, delta_to_best_ns: -910000000 },
};

const withLap = (lap: number, totalLaps = base.frame!.total_laps) => ({
  ...base,
  frame: { ...base.frame!, current_lap: lap, total_laps: totalLaps },
});

describe("deriveLive", () => {
  it("live + in-race -> true", () => expect(deriveLive(base)).toBe(true));
  it("status idle -> false", () => expect(deriveLive({ ...base, status: "idle", frame: null, derived: null })).toBe(false));
  it("null -> false", () => expect(deriveLive(null)).toBe(false));
  it("current_lap > total_laps -> false (race over)", () => expect(deriveLive(withLap(11, 10))).toBe(false));
  it("total_laps 0 -> true", () => expect(deriveLive(withLap(5, 0))).toBe(true));
});

describe("stepRace", () => {
  it("first frame: prev.raceKey null -> newRace false, raceKey adopted", () => {
    const { state, newRace } = stepRace(initialRaceKey, base);
    expect(newRace).toBe(false);
    expect(state.raceKey).toBe("a");
    expect(state.lastLap).toBe(8);
  });

  it('id "a"->"b" -> newRace true, raceKey "b"', () => {
    const prev = { raceKey: "a", lastLap: 8 };
    const s = { ...base, enrichment: { ...base.enrichment, session_id: "b" } };
    const { state, newRace } = stepRace(prev, s);
    expect(newRace).toBe(true);
    expect(state.raceKey).toBe("b");
  });

  it('id "a"->null -> newRace false, raceKey stays "a"', () => {
    const prev = { raceKey: "a", lastLap: 8 };
    const s = { ...base, enrichment: { ...base.enrichment, session_id: undefined } };
    const { state, newRace } = stepRace(prev, s);
    expect(newRace).toBe(false);
    expect(state.raceKey).toBe("a");
  });

  it("id null, lap 3->1 -> newRace true", () => {
    const prev = { raceKey: null, lastLap: 3 };
    const s = { ...withLap(1), enrichment: { ...base.enrichment, session_id: undefined } };
    const { newRace } = stepRace(prev, s);
    expect(newRace).toBe(true);
  });

  it("id null, lap 5->4 -> newRace false", () => {
    const prev = { raceKey: null, lastLap: 5 };
    const s = { ...withLap(4), enrichment: { ...base.enrichment, session_id: undefined } };
    const { newRace } = stepRace(prev, s);
    expect(newRace).toBe(false);
  });

  it("same session_id but lap resets 1 from >1 -> newRace true (rtps restart reused id)", () => {
    const prev = { raceKey: "a", lastLap: 8 };
    const s = { ...withLap(1), enrichment: { ...base.enrichment, session_id: "a" } };
    const { newRace } = stepRace(prev, s);
    expect(newRace).toBe(true);
  });
});
