// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { useLaps } from "./useLaps";
import type { LapDTO } from "../contract/session";

function stubFetch(payload: unknown, ok = true) {
  const spy = vi.fn((_u: string, _i?: RequestInit) =>
    Promise.resolve({ ok, status: ok ? 200 : 500, json: async () => payload }));
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
}
afterEach(() => vi.unstubAllGlobals());

const lap = (n: number): LapDTO => ({
  lap_number: n, duration_ns: 1, complete: true, valid: true, timed: true,
  delta_to_pb_ns: null, sectors: { reliable: true, reason: null, splits: [] },
});

describe("useLaps", () => {
  it("fetches laps for a session", async () => {
    stubFetch([lap(1), lap(2)]);
    const { result } = renderHook(() => useLaps("s1"));
    await waitFor(() => expect(result.current.laps.length).toBe(2));
    expect(result.current.loading).toBe(false);
  });
  it("null session -> empty, no fetch", () => {
    const spy = stubFetch([]);
    const { result } = renderHook(() => useLaps(null));
    expect(result.current.laps).toEqual([]);
    expect(spy).not.toHaveBeenCalled();
  });
  it("surfaces error on non-ok", async () => {
    stubFetch(null, false);
    const { result } = renderHook(() => useLaps("s1"));
    await waitFor(() => expect(result.current.error).toBe(true));
  });
});
