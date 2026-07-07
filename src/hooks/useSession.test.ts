// @vitest-environment jsdom
// src/hooks/useSession.test.ts
import { describe, it, expect, vi } from "vitest";
import { StrictMode } from "react";
import { act, renderHook } from "@testing-library/react";
import { useSession } from "./useSession";
import type { LiveHandlers, LiveSource } from "../lib/liveSource";
import type { LiveSnapshot } from "../contract/live";

const enr = { analysis: null, car_name: null, track: null, delta_to_best_ns: null };
const liveFrame = {
  car_speed: 214.3, current_gear: 4, suggested_gear: 5, rpm: 7200,
  throttle: 1, brake: 0, clutch: 0, boost: 1.2, current_fuel: 14.2, fuel_capacity: 60,
  tyre_temp_fl: 88, tyre_temp_fr: 96, tyre_temp_rl: 82, tyre_temp_rr: 90,
  current_lap: 8, total_laps: 20, current_position: 3, total_positions: 16,
  best_lap: "77.210s", last_lap: "78.004s", in_race: true, is_paused: false,
};
// Realistic snapshots (a live one has a real frame/derived, matching what subscribeLive would deliver).
const snap = (status: "live" | "idle"): LiveSnapshot =>
  status === "live"
    ? { served_at: "2026-07-07T00:00:00Z", status, frame: { ...liveFrame }, derived: { current_lap_time_ns: 77210000000, fuel_pct: 23.7, source: "LIVE" }, enrichment: { ...enr } }
    : { served_at: "2026-07-07T00:00:00Z", status, frame: null, derived: null, enrichment: { ...enr } };

// A controllable fake source: capture handlers, return a spy unsubscribe.
function makeFakeSource() {
  const captured: { handlers?: LiveHandlers; url?: string; subscribeCount: number } = { subscribeCount: 0 };
  const unsubscribe = vi.fn();
  const source: LiveSource = (url, handlers) => {
    captured.subscribeCount++;
    captured.url = url;
    captured.handlers = handlers;
    return unsubscribe;
  };
  return { source, captured, unsubscribe };
}

describe("useSession", () => {
  it("starts with null snapshot + null error", () => {
    const { source } = makeFakeSource();
    const { result } = renderHook(() => useSession("u", source));
    expect(result.current.snapshot).toBeNull();
    expect(result.current.error).toBeNull();
  });

  it("stores a pushed snapshot", () => {
    const { source, captured } = makeFakeSource();
    const { result } = renderHook(() => useSession("u", source));
    act(() => captured.handlers!.onSnapshot(snap("live")));
    expect(result.current.snapshot!.status).toBe("live");
  });

  it("keeps last-good snapshot but sets error on stream error", () => {
    const { source, captured } = makeFakeSource();
    const { result } = renderHook(() => useSession("u", source));
    act(() => captured.handlers!.onSnapshot(snap("live")));
    act(() => captured.handlers!.onError(new Error("boom")));
    expect(result.current.snapshot!.status).toBe("live"); // retained
    expect(result.current.error!.message).toBe("boom");
  });

  it("clears error when a fresh snapshot arrives", () => {
    const { source, captured } = makeFakeSource();
    const { result } = renderHook(() => useSession("u", source));
    act(() => captured.handlers!.onError(new Error("boom")));
    act(() => captured.handlers!.onSnapshot(snap("idle")));
    expect(result.current.error).toBeNull();
  });

  it("unsubscribes on unmount", () => {
    const { source, unsubscribe } = makeFakeSource();
    const { unmount } = renderHook(() => useSession("u", source));
    unmount();
    expect(unsubscribe).toHaveBeenCalledOnce();
  });

  it("re-subscribes when the url changes (old closed, new opened)", () => {
    const { source, captured, unsubscribe } = makeFakeSource();
    const { rerender } = renderHook(({ url }) => useSession(url, source), { initialProps: { url: "u1" } });
    rerender({ url: "u2" });
    expect(unsubscribe).toHaveBeenCalledOnce(); // old subscription torn down before the new one
    expect(captured.url).toBe("u2");
    expect(captured.subscribeCount).toBe(2);
  });

  it("ignores a late callback from the previous subscription after a url change", () => {
    const { source, captured } = makeFakeSource();
    const { result, rerender } = renderHook(({ url }) => useSession(url, source), { initialProps: { url: "u1" } });
    const stale = captured.handlers!;          // u1's handlers, captured before the url change
    rerender({ url: "u2" });                   // effect re-runs; u1's cleanup flips its active=false
    act(() => stale.onSnapshot(snap("live"))); // a late push from the torn-down u1 subscription
    expect(result.current.snapshot).toBeNull(); // guard held: the stale subscription cannot mutate state
  });

  it("leaks no subscription under StrictMode (unsubscribe count matches subscribe count)", () => {
    const { source, captured, unsubscribe } = makeFakeSource();
    const { unmount } = renderHook(() => useSession("u", source), { wrapper: StrictMode });
    unmount();
    expect(unsubscribe.mock.calls.length).toBe(captured.subscribeCount);
  });
});
