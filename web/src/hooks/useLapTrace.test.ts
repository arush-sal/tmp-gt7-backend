// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { useLapTrace } from "./useLapTrace";
import { putLap, resetLapCache } from "../lib/lapCache";
import type { LapSample } from "../lib/lapView";
import type { TraceSampleDTO } from "../contract/session";

function stubFetch(payload: unknown, ok = true) {
  const spy = vi.fn((_u: string, _i?: RequestInit) =>
    Promise.resolve({ ok, status: ok ? 200 : 500, json: async () => payload }));
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
}
afterEach(() => vi.unstubAllGlobals());
beforeEach(resetLapCache);

const traceDto = (i: number): TraceSampleDTO => ({
  seq: i, t_ns: i * 1e9, distance: i, pos_x: i, pos_z: i, throttle: 0, brake: 0, speed: 0, gear: 0,
  rpm: 0, fuel: 0, tyre_temp_fl: 0, tyre_temp_fr: 0, tyre_temp_rl: 0, tyre_temp_rr: 0, clutch: 0, surface_type: "",
  boost: 0, delta_to_best_ns: null, steering: 0,
});
const sample = (t: number): LapSample => ({
  t, posX: t, posZ: 0, surface: "TTTT", distance: 0, throttle: 0, brake: 0, clutch: 0, rpm: 0, gear: 0, speed: 0,
  boost: 0, delta: 0, tyreFL: 0, tyreFR: 0, tyreRL: 0, tyreRR: 0, fuel: 0, steering: 0,
});

describe("useLapTrace", () => {
  it("cache hit -> view from live samples, NO fetch", () => {
    const spy = stubFetch([]);
    putLap("s1", 2, [sample(1), sample(2)]);
    const { result } = renderHook(() => useLapTrace("s1", 2));
    expect(result.current.view?.length).toBe(2);
    expect(result.current.view?.points[0].surface).toBe("TTTT"); // full-fidelity live sample
    expect(spy).not.toHaveBeenCalled();
  });
  it("cache miss -> fetches decimated trace", async () => {
    const spy = stubFetch([traceDto(0), traceDto(1)]);
    const { result } = renderHook(() => useLapTrace("s1", 5));
    await waitFor(() => expect(result.current.view?.length).toBe(2));
    expect(spy).toHaveBeenCalledOnce();
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/laps/5/trace?decimate=1500");
  });
  it("warms the cache: a second mount for the same lap does not refetch", async () => {
    const spy = stubFetch([traceDto(0), traceDto(1)]);
    const { result, unmount } = renderHook(() => useLapTrace("s1", 7));
    await waitFor(() => expect(result.current.view?.length).toBe(2));
    expect(spy).toHaveBeenCalledOnce();
    unmount();
    const { result: r2 } = renderHook(() => useLapTrace("s1", 7));
    expect(r2.current.view?.length).toBe(2); // served from the warmed cache
    expect(spy).toHaveBeenCalledOnce();       // no second fetch
  });
  it("null lap -> no view, no fetch", () => {
    const spy = stubFetch([]);
    const { result } = renderHook(() => useLapTrace("s1", null));
    expect(result.current.view).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });
});
