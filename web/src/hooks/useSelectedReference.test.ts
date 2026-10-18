// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { useSelectedReference } from "./useSelectedReference";
import { storedRef } from "../mock/storedRef";

const predicted = storedRef({ source: "fastest", lap_number: 9 }); // stands in for the session ReferenceDTO
const stub = (status: number, body: unknown = {}) => {
  const spy = vi.fn(async (_u: string, _i?: RequestInit) => ({ ok: status < 400, status, json: async () => body }));
  vi.stubGlobal("fetch", spy as unknown as typeof fetch);
  return spy;
};
afterEach(() => vi.unstubAllGlobals());

describe("useSelectedReference", () => {
  it("stored source + stored lap exists -> stored wins", async () => {
    stub(200, storedRef());
    const { result } = renderHook(() => useSelectedReference("suzuka", "stored", predicted));
    await waitFor(() => expect(result.current.effective).toBe("stored"));
    expect((result.current.reference as { lap_number?: number }).lap_number).toBe(2);
  });
  it("predicted source -> session reference even when a stored lap exists", async () => {
    stub(200, storedRef());
    const { result } = renderHook(() => useSelectedReference("suzuka", "predicted", predicted));
    await waitFor(() => expect(result.current.stored).not.toBeNull());
    expect(result.current.reference).toBe(predicted);
    expect(result.current.effective).toBe("predicted");
  });
  it("404 -> falls back to predicted and flags noStored", async () => {
    stub(404);
    const { result } = renderHook(() => useSelectedReference("suzuka", "stored", predicted));
    await waitFor(() => expect(result.current.noStored).toBe(true));
    expect(result.current.reference).toBe(predicted);
    expect(result.current.effective).toBe("predicted");
  });
  it("empty slug -> no request", () => {
    const spy = stub(200, storedRef());
    const { result } = renderHook(() => useSelectedReference(null, "stored", predicted));
    expect(spy).not.toHaveBeenCalled();
    expect(result.current.reference).toBe(predicted);
  });
  it("refetches on slug change and on pinVersion change", async () => {
    const spy = stub(200, storedRef());
    const { rerender } = renderHook(({ s, v }) => useSelectedReference(s, "stored", predicted, v), { initialProps: { s: "a", v: 0 } });
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));
    rerender({ s: "b", v: 0 });
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(2));
    expect(spy.mock.calls[1][0]).toContain("/v1/tracks/b/");
    rerender({ s: "b", v: 1 });
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(3));
  });
  it("does not show the previous track's stored lap on a new slug", async () => {
    stub(200, storedRef());
    const { result, rerender } = renderHook(({ s }) => useSelectedReference(s, "stored", predicted), { initialProps: { s: "a" } });
    await waitFor(() => expect(result.current.stored).not.toBeNull());
    rerender({ s: "b" });
    expect(result.current.stored).toBeNull();
  });
  it("404 then a lap completion refetches (once, coalesced) and the stored ref appears", async () => {
    let status = 404;
    const spy = vi.fn(async (_u: string, _i?: RequestInit) => ({ ok: status < 400, status, json: async () => storedRef() }));
    vi.stubGlobal("fetch", spy as unknown as typeof fetch);
    const { result, rerender } = renderHook(({ k }) => useSelectedReference("suzuka", "stored", predicted, 0, k), { initialProps: { k: 0 } });
    await waitFor(() => expect(result.current.noStored).toBe(true));
    expect(spy).toHaveBeenCalledTimes(1);
    status = 200;
    rerender({ k: 1 });
    rerender({ k: 2 }); // a burst of refresh-key changes must coalesce into one fetch
    await waitFor(() => expect(result.current.effective).toBe("stored"));
    expect(spy).toHaveBeenCalledTimes(2);
    expect(result.current.noStored).toBe(false);
    await new Promise(r => setTimeout(r, 450));
    expect(spy).toHaveBeenCalledTimes(2);
  });
  it("500 is an error (not noStored) and yields NO reference, never the predicted fallback; 404 stays noStored", async () => {
    stub(500);
    const { result } = renderHook(() => useSelectedReference("suzuka", "stored", predicted));
    await waitFor(() => expect(result.current.error).toBe(true));
    expect(result.current.noStored).toBe(false);
    expect(result.current.reference).toBeNull();
    stub(404);
    const r2 = renderHook(() => useSelectedReference("suzuka", "stored", predicted));
    await waitFor(() => expect(r2.result.current.noStored).toBe(true));
    expect(r2.result.current.error).toBe(false);
  });
  it("a network error also surfaces; a predicted source is unaffected", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new TypeError("net"); }) as unknown as typeof fetch);
    const { result } = renderHook(() => useSelectedReference("suzuka", "predicted", predicted));
    await waitFor(() => expect(result.current.error).toBe(true));
    expect(result.current.reference).toBe(predicted);
  });

  describe("auto (fastest available)", () => {
    const pred = (ns: number, n = 3) => ({ ...predicted, predicted_best_ns: ns, samples: predicted.samples.slice(0, n) });
    it("only stored (predicted empty) -> stored", async () => {
      stub(200, storedRef());
      const { result } = renderHook(() => useSelectedReference("suzuka", "auto", pred(0, 0)));
      await waitFor(() => expect(result.current.effective).toBe("stored"));
      expect(result.current.reference).toBe(result.current.stored);
    });
    it("only predicted (no stored lap) -> predicted", async () => {
      stub(404);
      const p = pred(95e9);
      const { result } = renderHook(() => useSelectedReference("suzuka", "auto", p));
      await waitFor(() => expect(result.current.noStored).toBe(true));
      expect(result.current.reference).toBe(p);
      expect(result.current.effective).toBe("predicted");
    });
    it("both -> shorter lap time wins", async () => {
      stub(200, storedRef({ lap_time: "90.685s" }));
      const fast = pred(89e9);
      const a = renderHook(() => useSelectedReference("suzuka", "auto", fast));
      await waitFor(() => expect(a.result.current.stored).not.toBeNull());
      expect(a.result.current.effective).toBe("predicted");
      expect(a.result.current.reference).toBe(fast);
      const slow = pred(95e9);
      const b = renderHook(() => useSelectedReference("suzuka", "auto", slow));
      await waitFor(() => expect(b.result.current.stored).not.toBeNull());
      expect(b.result.current.effective).toBe("stored");
    });
    it("neither -> null", async () => {
      stub(404);
      const { result } = renderHook(() => useSelectedReference("suzuka", "auto", pred(0, 0)));
      await waitFor(() => expect(result.current.noStored).toBe(true));
      expect(result.current.reference).toBeNull();
    });
    it("stored load error falls back to predicted", async () => {
      stub(500);
      const p = pred(95e9);
      const { result } = renderHook(() => useSelectedReference("suzuka", "auto", p));
      await waitFor(() => expect(result.current.error).toBe(true));
      expect(result.current.reference).toBe(p);
      expect(result.current.effective).toBe("predicted");
    });
  });
});
