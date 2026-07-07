// @vitest-environment jsdom
// src/components/Dashboard.test.tsx
import { describe, it, expect } from "vitest";
import { render, screen, act } from "@testing-library/react";
import Dashboard from "./Dashboard";
import type { LiveSource, LiveHandlers } from "../lib/liveSource";
import { validateLive } from "../lib/validateLive";
import { liveSample } from "../mock/liveSample";

function fakeSource(): { source: LiveSource; push: () => void; fail: (m: string) => void } {
  let handlers: LiveHandlers | undefined;
  const source: LiveSource = (_url, h) => { handlers = h; return () => {}; };
  return {
    source,
    push: () => handlers!.onSnapshot(validateLive(liveSample)),
    fail: (m: string) => handlers!.onError(new Error(m)),
  };
}

describe("Dashboard (live)", () => {
  it("shows a connection banner before the first snapshot", () => {
    const { source } = fakeSource();
    render(<Dashboard source={source} />);
    expect(screen.getByText(/connecting|waiting|no telemetry/i)).toBeTruthy();
  });

  it("renders live values after a snapshot arrives", () => {
    const { source, push } = fakeSource();
    render(<Dashboard source={source} />);
    act(() => push());
    expect(screen.getByText("P1/20")).toBeTruthy();                        // position (header only)
    expect(screen.getByText("77/90")).toBeTruthy();                        // lap (header only)
    expect(screen.getAllByText("1:08.602").length).toBeGreaterThan(0);     // best lap (header + lap history)
    expect(screen.getByText("100%")).toBeTruthy();                         // fuel (right panel only)
    expect(screen.getByText("87°C")).toBeTruthy();                         // hot tyre rl (right panel only)
    expect(screen.queryByText(/connecting|waiting|no telemetry/i)).toBeNull();
  });

  it("keeps live values and surfaces the error banner on a stream error", () => {
    const { source, push, fail } = fakeSource();
    render(<Dashboard source={source} />);
    act(() => push());
    act(() => fail("boom"));
    expect(screen.getByText("P1/20")).toBeTruthy();               // last-good retained
    expect(screen.getByText(/stream error: boom/i)).toBeTruthy(); // error surfaced over live data
  });
});
