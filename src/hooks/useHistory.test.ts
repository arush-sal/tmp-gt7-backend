// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useHistory } from "./useHistory";
import type { LiveSource, LiveHandlers } from "../lib/liveSource";
import type { LiveSnapshot } from "../contract/live";

const live = (lap: number, ns: number): LiveSnapshot => ({
  served_at: "x", status: "live",
  frame: { car_speed: 1, current_gear: 1, suggested_gear: 1, rpm: 1, throttle: 42, brake: 0, clutch: 0,
    boost: 0, current_fuel: 1, fuel_capacity: 1, tyre_temp_fl: 1, tyre_temp_fr: 1, tyre_temp_rl: 1,
    tyre_temp_rr: 1, current_lap: lap, total_laps: 2, current_position: 1, total_positions: 1,
    best_lap: null, last_lap: null, in_race: true, is_paused: false },
  derived: { current_lap_time_ns: ns, fuel_pct: 1, source: "LIVE" },
  enrichment: { analysis: null, car_name: null, track: null, delta_to_best_ns: null },
});

describe("useHistory", () => {
  beforeEach(() => localStorage.clear());

  it("feeds pushed frames into the store; unsubscribes on unmount", () => {
    let handlers: LiveHandlers | undefined;
    const unsub = vi.fn();
    const source: LiveSource = (_u, h) => { handlers = h; return unsub; };
    const { result, unmount } = renderHook(() => useHistory(source));
    handlers!.onSnapshot(live(1, 1e9));
    handlers!.onSnapshot(live(1, 2e9));
    expect(result.current.size()).toBe(2);
    expect(result.current.columns(["throttle"])[1]).toEqual([42, 42]);
    unmount();
    expect(unsub).toHaveBeenCalledOnce();
  });

  it("caches a completed lap to lapCache keyed by (sessionId, lap) on rollover", async () => {
    const { getLap, resetLapCache } = await import("../lib/lapCache");
    resetLapCache();
    let handlers: LiveHandlers | undefined;
    const source: LiveSource = (_u, h) => { handlers = h; return vi.fn(); };
    renderHook(() => useHistory(source, "sess-x"));
    act(() => { handlers!.onSnapshot(live(1, 1e9)); handlers!.onSnapshot(live(1, 2e9)); });
    act(() => { handlers!.onSnapshot(live(2, 1e8)); }); // rollover -> lap 1 finished, cached

    const cached = getLap("sess-x", 1);
    expect(cached?.length).toBe(2);
    expect(cached?.map(s => s.throttle)).toEqual([42, 42]);
  });
});
