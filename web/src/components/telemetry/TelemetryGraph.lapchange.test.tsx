// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, act } from "@testing-library/react";
import type { LapView } from "../../lib/lapView";
import { createHistoryStore } from "../../lib/historyStore";

// Minimal uPlot double: records data + x-scale so the test can see what the chart would draw.
const { FakePlot, instances } = vi.hoisted(() => {
  const instances: InstanceType<typeof FakePlot>[] = [];
  class FakePlot {
    data: unknown[][]; scaleX: { min: number; max: number } | null = null;
    over = { addEventListener() {} }; bbox = { height: 100 }; cursor = {}; select = {}; series = [];
    constructor(_o: unknown, data: unknown[][]) { this.data = data; instances.push(this); }
    setData(d: unknown[][]) { this.data = d; }
    setScale(_k: string, r: { min: number; max: number }) { this.scaleX = r; }
    setSize() {} setCursor() {} setSeries() {} setSelect() {} destroy() {}
    valToPos(v: number) { return v; } posToVal(v: number) { return v; }
    static sync() { return { key: "k" }; }
  }
  return { FakePlot, instances };
});
vi.mock("uplot", () => ({ default: FakePlot }));
import TelemetryGraph from "./TelemetryGraph";

const lap = (ts: number[]): LapView => ({
  points: ts.map(t => ({ t })) as never, length: ts.length,
  columns: chs => [ts, ...chs.map(() => ts.map(() => 1))],
  fractions: () => ts.map((_, i) => i / Math.max(1, ts.length - 1)),
  frameAt: () => null,
});

beforeEach(() => {
  instances.length = 0;
  // Fire once with a real size so the chart is constructed.
  vi.stubGlobal("ResizeObserver", class {
    cb: ResizeObserverCallback;
    constructor(cb: ResizeObserverCallback) { this.cb = cb; }
    observe() { this.cb([{ contentRect: { width: 300, height: 100 } } as ResizeObserverEntry], this as never); }
    unobserve() {} disconnect() {}
  });
  vi.stubGlobal("requestAnimationFrame", () => 1); // no tick: the lap-change path alone must refit
  vi.stubGlobal("cancelAnimationFrame", () => {});
});
afterEach(() => vi.unstubAllGlobals());

describe("TelemetryGraph lap change", () => {
  it("refits the x-scale to the NEW lap (not the previous lap's data) without a remount", () => {
    const store = createHistoryStore();
    const { rerender } = render(<TelemetryGraph store={store} channels={["steering"]} label="Steering" analysisLap={lap([0, 0.1, 0.15])} />);
    act(() => rerender(<TelemetryGraph store={store} channels={["steering"]} label="Steering" analysisLap={lap([0, 45, 90])} />));
    expect(instances).toHaveLength(1);
    expect(instances[0].scaleX).toEqual({ min: 0, max: 90 });
  });
});
