// src/lib/validateLive.test.ts
import { describe, it, expect } from "vitest";
import { validateLive } from "./validateLive";
import type { Frame, Derived, Enrichment } from "../contract/live";

// Typed fixtures so tests can mutate fields to their full contract types
// (e.g. best_lap -> null, delta_to_best_ns -> number) and still typecheck.
const frame: Frame = {
  car_speed: 214.3, current_gear: 4, suggested_gear: 5, rpm: 7200,
  throttle: 1, brake: 0, clutch: 0, boost: 1.2,
  current_fuel: 14.2, fuel_capacity: 60,
  tyre_temp_fl: 88, tyre_temp_fr: 96, tyre_temp_rl: 82, tyre_temp_rr: 90,
  current_lap: 8, total_laps: 20, current_position: 3, total_positions: 16,
  best_lap: "77.210s", last_lap: "78.004s", in_race: true, is_paused: false,
};
const derived: Derived = { current_lap_time_ns: 77210000000, fuel_pct: 23.7, source: "LIVE" };
const enrichment: Enrichment = { analysis: null, car_name: null, track: null, delta_to_best_ns: null };
const live = () => ({ served_at: "2026-07-07T00:00:00Z", status: "live", frame: { ...frame }, derived: { ...derived }, enrichment: { ...enrichment } });
const idle = () => ({ served_at: "2026-07-07T00:00:00Z", status: "idle", frame: null, derived: null, enrichment: { ...enrichment } });

describe("validateLive — happy paths", () => {
  it("accepts a valid live snapshot", () => expect(validateLive(live()).status).toBe("live"));
  it("accepts a valid idle snapshot", () => expect(validateLive(idle()).frame).toBeNull());
  it("accepts null best_lap/last_lap", () => {
    const s = live(); s.frame.best_lap = null; s.frame.last_lap = null;
    expect(validateLive(s).frame!.best_lap).toBeNull();
  });
  it("accepts a numeric delta_to_best_ns", () => {
    const s = live(); s.enrichment.delta_to_best_ns = -910000000;
    expect(validateLive(s).enrichment.delta_to_best_ns).toBe(-910000000);
  });
  it("accepts analysis as null or object", () => {
    const s = live(); s.enrichment.analysis = { session_id: "x" };
    expect(validateLive(s).enrichment.analysis).toEqual({ session_id: "x" });
  });
  it("accepts optional session_id present", () => {
    const s = live(); (s.enrichment as any).session_id = "sess-6";
    expect(validateLive(s).enrichment.session_id).toBe("sess-6");
  });
  it("is lenient on unknown extra frame fields", () => {
    const s = live(); (s.frame as any).oil_temp = 90;
    expect(() => validateLive(s)).not.toThrow();
  });
  it("is lenient on unknown extra enrichment/derived/top-level fields", () => {
    const s = live();
    (s.enrichment as any).future_field = 1;
    (s.derived as any).future_field = 2;
    (s as any).future_field = 3;
    expect(() => validateLive(s)).not.toThrow();
  });
});

describe("validateLive — rejections", () => {
  it("rejects non-object", () => expect(() => validateLive(null)).toThrow());
  it("rejects bad status", () => { const s = live(); (s as any).status = "paused"; expect(() => validateLive(s)).toThrow(); });
  it("rejects live with null frame", () => { const s = live(); (s as any).frame = null; expect(() => validateLive(s)).toThrow(/live/); });
  it("rejects idle with a frame present", () => { const s = idle(); (s as any).frame = { ...frame }; expect(() => validateLive(s)).toThrow(/idle/); });
  it("rejects missing required enrichment key", () => { const s = live(); delete (s.enrichment as any).delta_to_best_ns; expect(() => validateLive(s)).toThrow(/delta_to_best_ns/); });
  it("rejects session_id of wrong type", () => { const s = live(); (s.enrichment as any).session_id = 5; expect(() => validateLive(s)).toThrow(/session_id/); });
  it("rejects malformed best_lap", () => { const s = live(); s.frame.best_lap = "77.21"; expect(() => validateLive(s)).toThrow(/best_lap/); });
  it("rejects a wrong-typed frame number", () => { const s = live(); (s.frame as any).car_speed = "fast"; expect(() => validateLive(s)).toThrow(/car_speed/); });
  it("rejects unsafe-integer ns", () => { const s = live(); s.derived.current_lap_time_ns = Number.MAX_SAFE_INTEGER + 2; expect(() => validateLive(s)).toThrow(/current_lap_time_ns/); });
  it("rejects NaN fuel_pct", () => { const s = live(); (s.derived as any).fuel_pct = NaN; expect(() => validateLive(s)).toThrow(/fuel_pct/); });
  it("rejects wrong-typed source", () => { const s = live(); (s.derived as any).source = 1; expect(() => validateLive(s)).toThrow(/source/); });
  it("rejects wrong-typed car_name", () => { const s = live(); (s.enrichment as any).car_name = 5; expect(() => validateLive(s)).toThrow(/car_name/); });
  it("rejects wrong-typed track", () => { const s = live(); (s.enrichment as any).track = 5; expect(() => validateLive(s)).toThrow(/track/); });
  it("rejects non-object analysis", () => { const s = live(); (s.enrichment as any).analysis = "nope"; expect(() => validateLive(s)).toThrow(/analysis/); });
  it("rejects live with null derived", () => { const s = live(); (s as any).derived = null; expect(() => validateLive(s)).toThrow(/derived/); });
  it("rejects idle with a derived present", () => { const s = idle(); (s as any).derived = { current_lap_time_ns: 1, fuel_pct: 1, source: "LIVE" }; expect(() => validateLive(s)).toThrow(/derived/); });
  it("rejects array analysis", () => { const s = live(); (s.enrichment as any).analysis = []; expect(() => validateLive(s)).toThrow(/analysis/); });
  it("rejects missing car_name key", () => { const s = live(); delete (s.enrichment as any).car_name; expect(() => validateLive(s)).toThrow(/car_name/); });
  it("rejects missing track key", () => { const s = live(); delete (s.enrichment as any).track; expect(() => validateLive(s)).toThrow(/track/); });
  it("rejects missing analysis key", () => { const s = live(); delete (s.enrichment as any).analysis; expect(() => validateLive(s)).toThrow(/analysis/); });
});
