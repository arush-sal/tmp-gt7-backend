// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { parseMode, useMode } from "./useMode";

afterEach(() => vi.unstubAllGlobals());
const stub = (impl: (u: string) => Promise<unknown>) => {
  const spy = vi.fn(impl);
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
};

describe("parseMode", () => {
  it("accepts the three modes", () => {
    for (const m of ["live", "replay", "analysis"]) expect(parseMode({ mode: m })).toBe(m);
  });
  it("falls back to live for anything else", () => {
    for (const v of [null, "analysis", {}, { mode: "ANALYSIS" }, { mode: 1 }]) expect(parseMode(v)).toBe("live");
  });
});

describe("useMode", () => {
  it("is null until /v1/mode resolves, then the mode", async () => {
    const spy = stub(async () => ({ ok: true, status: 200, json: async () => ({ mode: "analysis" }) }));
    const { result } = renderHook(() => useMode());
    expect(result.current).toBeNull();
    await waitFor(() => expect(result.current).toBe("analysis"));
    expect(spy.mock.calls[0][0]).toBe("/v1/mode");
  });
  it("non-2xx -> live", async () => {
    stub(async () => ({ ok: false, status: 404, json: async () => ({ mode: "analysis" }) }));
    const { result } = renderHook(() => useMode());
    await waitFor(() => expect(result.current).toBe("live"));
  });
  it("network failure -> live", async () => {
    stub(async () => { throw new TypeError("net"); });
    const { result } = renderHook(() => useMode());
    await waitFor(() => expect(result.current).toBe("live"));
  });
});
