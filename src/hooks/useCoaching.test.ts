// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { useCoaching } from "./useCoaching";

function stubFetch(payload: unknown, ok = true) {
  const spy = vi.fn((_u: string, _i?: RequestInit) =>
    Promise.resolve({ ok, status: ok ? 200 : 409, json: async () => payload }));
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
}
afterEach(() => vi.unstubAllGlobals());
const report = (ns: number | undefined) => ({ analysis: ns === undefined ? null : { predicted_best_ns: ns }, coaching: { status: "pending", note: "" } });

describe("useCoaching", () => {
  it("fetches predicted-best when enabled (post-race)", async () => {
    stubFetch(report(115_100_000_000));
    const { result } = renderHook(() => useCoaching("s1", true));
    await waitFor(() => expect(result.current.predicted).toBe("1:55.100"));
  });
  it("disabled (live) -> null, no fetch", () => {
    const spy = stubFetch(report(1));
    const { result } = renderHook(() => useCoaching("s1", false));
    expect(result.current.predicted).toBeNull();
    expect(spy).not.toHaveBeenCalled();
  });
  it("no prediction (0 / null analysis) -> null", async () => {
    stubFetch(report(0));
    const { result } = renderHook(() => useCoaching("s1", true));
    await waitFor(() => expect(result.current.predicted).toBeNull());
  });
  it("error / 409 active -> null", async () => {
    stubFetch(null, false);
    const { result } = renderHook(() => useCoaching("s1", true));
    await waitFor(() => expect(result.current.predicted).toBeNull());
  });
});
