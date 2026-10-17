import { describe, it, expect, beforeEach, vi } from "vitest";
import { loadGeom, saveGeom, clampGeom, clampDockWidth, dockMaxWidth, loadDockWidth, saveDockWidth, type PanelGeom } from "./panelGeometry";

const D: PanelGeom = { x: 10, y: 10, width: 320, height: 260 };

describe("panelGeometry", () => {
  beforeEach(() => {
    const store: Record<string, string> = {};
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store[k] ?? null,
      setItem: (k: string, v: string) => { store[k] = v; },
    });
  });
  it("returns the fallback when nothing stored", () => {
    expect(loadGeom("k", D)).toEqual(D);
  });
  it("round-trips a saved geometry", () => {
    saveGeom("k", { x: 5, y: 6, width: 400, height: 300 });
    expect(loadGeom("k", D)).toEqual({ x: 5, y: 6, width: 400, height: 300 });
  });
  it("falls back on a corrupt/partial stored value", () => {
    localStorage.setItem("k", JSON.stringify({ x: 1 }));
    expect(loadGeom("k", D)).toEqual(D);
  });
  it("falls back on non-JSON", () => {
    localStorage.setItem("k", "{not json");
    expect(loadGeom("k", D)).toEqual(D);
  });
  it("falls back on primitive / empty stored values", () => {
    for (const v of ["1", "null", "\"x\"", ""]) {
      localStorage.setItem("k", v);
      expect(loadGeom("k", D)).toEqual(D);
    }
  });
  it("falls back (no throw) when getItem throws", () => {
    vi.stubGlobal("localStorage", { getItem: () => { throw new Error("blocked"); }, setItem: () => {} });
    expect(loadGeom("k", D)).toEqual(D);
  });
  it("saveGeom does not throw when setItem throws", () => {
    vi.stubGlobal("localStorage", { getItem: () => null, setItem: () => { throw new Error("full"); } });
    expect(() => saveGeom("k", D)).not.toThrow();
  });
});

describe("clampGeom", () => {
  it("leaves an in-bounds geometry unchanged", () => {
    const g: PanelGeom = { x: 10, y: 10, width: 320, height: 260 };
    expect(clampGeom(g, 1280, 800, 240, 180)).toEqual(g);
  });

  it("pulls a geometry fully off-right/below back in-bounds on a small viewport", () => {
    const g: PanelGeom = { x: 900, y: 900, width: 320, height: 260 };
    const c = clampGeom(g, 400, 300, 100, 100);
    expect(c.x + c.width).toBeLessThanOrEqual(400);
    expect(c.y).toBeLessThanOrEqual(300 - 40);
    expect(c.x).toBeGreaterThanOrEqual(0);
    expect(c.y).toBeGreaterThanOrEqual(0);
  });

  it("caps width/height to the viewport", () => {
    const g: PanelGeom = { x: 0, y: 0, width: 2000, height: 1500 };
    const c = clampGeom(g, 800, 600, 100, 100);
    expect(c.width).toBe(800);
    expect(c.height).toBe(600);
  });

  it("never returns a negative x or y", () => {
    const g: PanelGeom = { x: -50, y: -30, width: 320, height: 260 };
    const c = clampGeom(g, 1280, 800, 240, 180);
    expect(c.x).toBeGreaterThanOrEqual(0);
    expect(c.y).toBeGreaterThanOrEqual(0);
  });
});

describe("dock width", () => {
  beforeEach(() => {
    const store: Record<string, string> = {};
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store[k] ?? null,
      setItem: (k: string, v: string) => { store[k] = v; },
    });
  });
  it("clamps to [280, available - 330], min wins when space is small", () => {
    expect(clampDockWidth(100, 1000)).toBe(280);
    expect(clampDockWidth(900, 1000)).toBe(670);
    expect(clampDockWidth(400, 1000)).toBe(400);
    expect(clampDockWidth(400, 300)).toBe(280);
    expect(dockMaxWidth(0)).toBe(280);
  });
  it("defaults to 380 when missing or bad", () => {
    expect(loadDockWidth()).toBe(380);
    localStorage.setItem("gt7:telemetry-width", "abc");
    expect(loadDockWidth()).toBe(380);
    localStorage.setItem("gt7:telemetry-width", "-5");
    expect(loadDockWidth()).toBe(380);
    localStorage.setItem("gt7:telemetry-width", "0");
    expect(loadDockWidth()).toBe(380);
  });
  it("round-trips the preference without viewport clamping, floored at min", () => {
    saveDockWidth(450.4);
    expect(loadDockWidth()).toBe(450);
    saveDockWidth(2000);
    expect(loadDockWidth()).toBe(2000);
    localStorage.setItem("gt7:telemetry-width", "100");
    expect(loadDockWidth()).toBe(280);
  });
});
