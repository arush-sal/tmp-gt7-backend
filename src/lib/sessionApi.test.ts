import { describe, it, expect, vi, afterEach } from "vitest";
import { fetchLaps, fetchLapTrace, fetchReference } from "./sessionApi";

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
  it("fetchReference builds channels + points query", async () => {
    const spy = stubFetch({ fractions: [], series: {}, lap_count: 0 });
    await fetchReference("s1", ["throttle", "brake"], 500);
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/reference?channels=throttle%2Cbrake&points=500");
  });
  it("fetchReference omits query when no channels/points", async () => {
    const spy = stubFetch({ fractions: [], series: {}, lap_count: 0 });
    await fetchReference("s1", undefined, undefined);
    expect(spy.mock.calls[0][0]).toBe("/v1/sessions/s1/reference");
  });
  it("throws on non-ok response", async () => {
    stubFetch(null, false);
    await expect(fetchLaps("s1")).rejects.toThrow();
  });
});
