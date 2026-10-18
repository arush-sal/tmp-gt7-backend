// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { usePlayback } from "./usePlayback";

// Fake rAF on a manual clock: each `frame(ms)` advances time and runs the queued callbacks once.
let now = 0;
let queue: FrameRequestCallback[] = [];
const frame = (ms: number) => { now += ms; const q = queue; queue = []; act(() => q.forEach(cb => cb(now))); };
beforeEach(() => {
  now = 1000; queue = [];
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => { queue.push(cb); return queue.length; });
  vi.stubGlobal("cancelAnimationFrame", () => { queue = []; });
});
afterEach(() => vi.unstubAllGlobals());

const times = [10, 11, 12, 13]; // lap-time seconds (need not start at 0); 3s long

describe("usePlayback", () => {
  it("advances the playhead in real time and emits the sample index at it", () => {
    const onIndex = vi.fn();
    const { result } = renderHook(() => usePlayback(times, onIndex));
    expect(result.current.canPlay).toBe(true);
    act(() => result.current.toggle());
    expect(result.current.playing).toBe(true);
    frame(16);            // first frame anchors the clock at 0
    expect(onIndex).toHaveBeenLastCalledWith(0);
    frame(1500);          // 1.5s in -> sample at t=11
    expect(onIndex).toHaveBeenLastCalledWith(1);
    frame(600);           // 2.1s -> t=12
    expect(onIndex).toHaveBeenLastCalledWith(2);
  });

  it("pause keeps the playhead; play resumes from it", () => {
    const onIndex = vi.fn();
    const { result } = renderHook(() => usePlayback(times, onIndex));
    act(() => result.current.toggle());
    frame(16); frame(1500);
    act(() => result.current.toggle());
    expect(result.current.playing).toBe(false);
    frame(5000); // nothing scheduled while paused
    expect(onIndex).toHaveBeenLastCalledWith(1);
    act(() => result.current.toggle());
    frame(16);   // re-anchor; playhead still 1.5s
    expect(onIndex).toHaveBeenLastCalledWith(1);
    frame(600);
    expect(onIndex).toHaveBeenLastCalledWith(2);
  });

  it("stops at the end; Play again restarts from 0", () => {
    const onIndex = vi.fn();
    const { result } = renderHook(() => usePlayback(times, onIndex));
    act(() => result.current.toggle());
    frame(16); frame(5000);
    expect(onIndex).toHaveBeenLastCalledWith(3);
    expect(result.current.playing).toBe(false);
    act(() => result.current.toggle());
    frame(16);
    expect(onIndex).toHaveBeenLastCalledWith(0);
  });

  it("a new lap (times identity) stops and resets the playhead", () => {
    const onIndex = vi.fn();
    const { result, rerender } = renderHook(({ ts }) => usePlayback(ts, onIndex), { initialProps: { ts: times } });
    act(() => result.current.toggle());
    frame(16); frame(2100);
    rerender({ ts: [0, 1, 2] });
    expect(result.current.playing).toBe(false);
    act(() => result.current.toggle());
    frame(16);
    expect(onIndex).toHaveBeenLastCalledWith(0);
  });

  it("cannot play without a lap (or a single sample)", () => {
    const onIndex = vi.fn();
    const { result, rerender } = renderHook(({ ts }) => usePlayback(ts, onIndex), { initialProps: { ts: null as number[] | null } });
    expect(result.current.canPlay).toBe(false);
    act(() => result.current.toggle());
    expect(result.current.playing).toBe(false);
    rerender({ ts: [5] });
    expect(result.current.canPlay).toBe(false);
  });

  it("cancels the frame loop on unmount", () => {
    const cancel = vi.fn();
    vi.stubGlobal("cancelAnimationFrame", cancel);
    const { result, unmount } = renderHook(() => usePlayback(times, vi.fn()));
    act(() => result.current.toggle());
    frame(16);
    unmount();
    expect(cancel).toHaveBeenCalled();
  });
});
