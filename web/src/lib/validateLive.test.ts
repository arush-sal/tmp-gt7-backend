// src/lib/validateLive.test.ts
import { describe, it, expect } from "vitest";
import { validateLive } from "./validateLive";
import type { Frame, Derived, Enrichment } from "../contract/live";
import { liveSample } from "../mock/liveSample";

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
  it("accepts optional duplicate_of present, omits when absent", () => {
    const s = live(); (s.enrichment as any).duplicate_of = "sess-1";
    expect(validateLive(s).enrichment.duplicate_of).toBe("sess-1");
    expect("duplicate_of" in validateLive(live()).enrichment).toBe(false);
  });
  it("rejects duplicate_of of wrong type", () => { const s = live(); (s.enrichment as any).duplicate_of = 5; expect(() => validateLive(s)).toThrow(/duplicate_of/); });
  it("accepts optional recording boolean, omits when absent, rejects wrong type", () => {
    const s = live(); (s.enrichment as any).recording = false;
    expect(validateLive(s).enrichment.recording).toBe(false);
    expect("recording" in validateLive(live()).enrichment).toBe(false);
    const bad = live(); (bad.enrichment as any).recording = "yes";
    expect(() => validateLive(bad)).toThrow(/recording/);
  });
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

describe("validateLive - track_source / pending_track_slug", () => {
  it("passes both through when valid, omits when absent, drops wrong types", () => {
    const s = live(); Object.assign(s.enrichment, { track_source: "user", pending_track_slug: "spa" });
    expect(validateLive(s).enrichment).toMatchObject({ track_source: "user", pending_track_slug: "spa" });
    const n = live(); Object.assign(n.enrichment, { track_source: null, pending_track_slug: null });
    expect(validateLive(n).enrichment).toMatchObject({ track_source: null, pending_track_slug: null });
    expect("track_source" in validateLive(live()).enrichment).toBe(false);
    const bad = live(); Object.assign(bad.enrichment, { track_source: 5, pending_track_slug: 7 });
    expect("track_source" in validateLive(bad).enrichment).toBe(false);
    expect("pending_track_slug" in validateLive(bad).enrichment).toBe(false);
  });
});

describe("validateLive — format-C frame fields", () => {
  it("exposes format-C fields when the source provides them", () => {
    const s = live();
    Object.assign(s.frame, {
      wheel_steering_angle_front_left: 0.12, wheel_steering_angle_front_right: 0.11,
      wheel_base: 2.6, throttle_filtered: 170, surface_type: "TTTT", car_category: "GRN",
      current_lap_time_ms: 45000, sway: 1.4, heave: -0.2, surge: -0.8,
      wheel_rotation: 0.9, steering_angular_velocity: 0.3,
    });
    const f = validateLive(s).frame!;
    expect(f.surface_type).toBe("TTTT");
    expect(f.car_category).toBe("GRN");
    expect(f.wheel_steering_angle_front_left).toBeCloseTo(0.12);
    expect(f.wheel_base).toBeCloseTo(2.6);
    expect(f.current_lap_time_ms).toBe(45000);
    expect(f.surge).toBeCloseTo(-0.8);
  });
  it("omits format-C fields when a source lacks them (lenient, older/format-A sources)", () => {
    const f = validateLive(live()).frame!; // base fixture carries none
    expect(f.surface_type).toBeUndefined();
    expect(f.wheel_base).toBeUndefined();
    expect(f.current_lap_time_ms).toBeUndefined();
  });
  it("ignores wrong-typed format-C fields rather than throwing", () => {
    const s = live();
    (s.frame as any).surface_type = 5;      // wrong type
    (s.frame as any).wheel_base = "nope";   // wrong type
    expect(() => validateLive(s)).not.toThrow();
    const f = validateLive(s).frame!;
    expect(f.surface_type).toBeUndefined();
    expect(f.wheel_base).toBeUndefined();
  });
});

describe("validateLive — spatial + track_confidence fields (P2)", () => {
  it("accepts position + track_confidence when present", () => {
    const s = structuredClone(liveSample) as any;
    s.frame.position_x = -82.4;
    s.frame.position_y = 1.1;
    s.frame.position_z = -385.0;
    s.enrichment.track_confidence = "confident";
    expect(() => validateLive(s)).not.toThrow();
    const out = validateLive(s);
    expect(out.frame!.position_x).toBeCloseTo(-82.4);
    expect(out.frame!.position_y).toBeCloseTo(1.1);
    expect(out.frame!.position_z).toBeCloseTo(-385.0);
    expect(out.enrichment.track_confidence).toBe("confident");
  });

  it("accepts the provisional track_confidence (best guess, not yet locked)", () => {
    const s = structuredClone(liveSample) as any;
    s.enrichment.track = "sardegna-road-a";
    s.enrichment.track_confidence = "provisional";
    const out = validateLive(s);
    expect(out.enrichment.track).toBe("sardegna-road-a");
    expect(out.enrichment.track_confidence).toBe("provisional");
  });

  it("stays lenient when position + track_confidence absent", () => {
    const s = structuredClone(liveSample) as any;
    delete s.frame.position_x;
    delete s.frame.position_y;
    delete s.frame.position_z;
    delete s.enrichment.track_confidence;
    expect(() => validateLive(s)).not.toThrow();
    const out = validateLive(s);
    expect(out.frame!.position_x).toBeUndefined();
    expect(out.frame!.position_y).toBeUndefined();
    expect(out.frame!.position_z).toBeUndefined();
  });
});
