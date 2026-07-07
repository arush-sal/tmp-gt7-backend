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
  it("evicts only samples older than the cap window (keeps the rest)", () => {
    const s = createHistoryStore(10); // 10ms rolling window
    s.push(frame(1, 0, 10));          // t=0ms
    s.push(frame(1, 8_000_000, 20));  // t=8ms
    s.push(frame(1, 15_000_000, 30)); // t=15ms -> cutoff = 15-10 = 5ms; only t=0 is older
    expect(s.size()).toBe(2);         // 8ms + 15ms kept, 0ms evicted
  });
});
