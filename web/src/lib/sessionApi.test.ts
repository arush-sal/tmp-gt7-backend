import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchLaps, fetchLapTrace, fetchReference, fetchMedian, fetchPeaks } from "./sessionApi";

function stubFetch(payload: unknown, ok = true) {
  const spy = vi.fn((_url: string, _init?: RequestInit) =>
    Promise.resolve({ ok, status: ok ? 200 : 500, json: async () => payload }));
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
}
afterEach(() => vi.unstubAllGlobals());

describe("sessionApi", () => {
  it("fetchLaps hits /v1/sessions/{id}/laps", async () => {
    const spy = stubFetch([]);
    await fetchLaps("sess-abc");
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/sess-abc/laps");
  });
  it("fetchLapTrace includes lap + decimate", async () => {
    const spy = stubFetch([]);
    await fetchLapTrace("s1", 3, 1500);
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/laps/3/trace?decimate=1500");
  });
  it("fetchReference hits /reference with default decimate", async () => {
    const spy = stubFetch({ reliable: true, reason: null, predicted_best_ns: 0, sectors: [], samples: [] });
    await fetchReference("s1");
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/reference?decimate=1500");
  });
  it("fetchMedian builds channels + points query", async () => {
    const spy = stubFetch({ fractions: [], series: {}, lap_count: 0 });
    await fetchMedian("s1", ["throttle", "brake"], 500);
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/median?channels=throttle%2Cbrake&points=500");
  });
  it("fetchMedian omits query when no channels/points", async () => {
    const spy = stubFetch({ fractions: [], series: {}, lap_count: 0 });
    await fetchMedian("s1", undefined, undefined);
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/median");
  });
  it("fetchPeaks omits reference for predicted (A)", async () => {
    const spy = stubFetch({ points: [] });
    await fetchPeaks("s1", 5);
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/laps/5/peaks");
  });
  it("fetchPeaks sets reference for a lap (B)", async () => {
    const spy = stubFetch({ points: [] });
    await fetchPeaks("s1", 5, 3);
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/laps/5/peaks?reference=3");
  });
  it("throws on non-ok response", async () => {
    stubFetch(null, false);
    await expect(fetchLaps("s1")).rejects.toThrow();
  });
});
