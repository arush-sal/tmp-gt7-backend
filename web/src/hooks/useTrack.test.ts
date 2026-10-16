// @vitest-environment jsdom
import { renderHook, waitFor } from "@testing-library/react";
import { useTrack } from "./useTrack";

test("useTrack fetches and returns the record", async () => {
  const rec = { corners: [], confidence: { coordinate_usability: "generated-reliable" } };
  vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, json: async () => rec })) as any);
  const { result } = renderHook(() => useTrack("suzuka"));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.record).toEqual(rec);
  vi.unstubAllGlobals();
});

test("useTrack(null) does nothing", () => {
  const { result } = renderHook(() => useTrack(null));
  expect(result.current.record).toBeNull();
  expect(result.current.loading).toBe(false);
});

test("useTrack cache hit does not refetch on remount", async () => {
  const rec = { corners: [], confidence: { coordinate_usability: "generated-reliable" } };
  const fetchMock = vi.fn(async () => ({ ok: true, json: async () => rec })) as any;
  vi.stubGlobal("fetch", fetchMock);

  const first = renderHook(() => useTrack("cache-hit-track"));
  await waitFor(() => expect(first.result.current.loading).toBe(false));
  expect(fetchMock).toHaveBeenCalledTimes(1);
  first.unmount();

  const second = renderHook(() => useTrack("cache-hit-track"));
  expect(second.result.current.record).toEqual(rec);
  expect(second.result.current.loading).toBe(false);
  expect(fetchMock).toHaveBeenCalledTimes(1);

  vi.unstubAllGlobals();
});

test("useTrack non-ok response yields error state", async () => {
  vi.stubGlobal("fetch", vi.fn(async () => ({ ok: false, status: 404, json: async () => ({}) })) as any);
  const { result } = renderHook(() => useTrack("missing-track-404"));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.record).toBeNull();
  expect(result.current.error).toBe(true);
  vi.unstubAllGlobals();
});

test("useTrack ignores a stale response from a superseded slug", async () => {
  const recA = { corners: [], confidence: { coordinate_usability: "a" } } as any;
  const recB = { corners: [], confidence: { coordinate_usability: "b" } } as any;

  let resolveA: (v: any) => void = () => {};
  const fetchMock = vi.fn((url: string) => {
    if (url.includes("slug-a")) {
      return new Promise((resolve) => { resolveA = () => resolve({ ok: true, json: async () => recA }); });
    }
    return Promise.resolve({ ok: true, json: async () => recB });
  }) as any;
  vi.stubGlobal("fetch", fetchMock);

  const { result, rerender } = renderHook(({ slug }) => useTrack(slug), { initialProps: { slug: "slug-a" } });

  rerender({ slug: "slug-b" });
  await waitFor(() => expect(result.current.record).toEqual(recB));

  // Now let the stale slug-a fetch resolve after slug-b has already settled.
  resolveA({ ok: true, json: async () => recA });
  // Flush the .then/.then/.catch microtask chain (needs several ticks past
  // a macrotask boundary to fully settle) before asserting nothing changed.
  await new Promise((resolve) => setTimeout(resolve, 20));

  expect(result.current.record).toEqual(recB);

  vi.unstubAllGlobals();
});
