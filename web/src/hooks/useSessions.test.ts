// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import { useSessions } from "./useSessions";

const s = (id: string, start: string) => ({ id, car_id: 1, car_name: "C", status: "finished", start_time: start,
  end_time: start, frame_count: 1, gap_count: 0, current_lap: 3, total_laps: 0, best_lap: null });
function stubFetch(payload: unknown, ok = true) {
  const spy = vi.fn((_u: string, _i?: RequestInit) =>
    Promise.resolve({ ok, status: ok ? 200 : 500, json: async () => payload }));
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
}
afterEach(() => vi.unstubAllGlobals());

describe("useSessions", () => {
  it("fetches on mount, newest first", async () => {
    const spy = stubFetch([s("old", "2026-01-01T00:00:00Z"), s("new", "2026-02-01T00:00:00Z")]);
    const { result } = renderHook(() => useSessions());
    await waitFor(() => expect(result.current.sessions.map(x => x.id)).toEqual(["new", "old"]));
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions");
  });
  it("refresh refetches", async () => {
    const spy = stubFetch([]);
    const { result } = renderHook(() => useSessions());
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    act(() => result.current.refresh());
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(2));
  });
  it("surfaces error on non-ok", async () => {
    stubFetch(null, false);
    const { result } = renderHook(() => useSessions());
    await waitFor(() => expect(result.current.error).toBe(true));
  });
});
