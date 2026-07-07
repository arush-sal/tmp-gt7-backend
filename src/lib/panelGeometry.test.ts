import { describe, it, expect, beforeEach, vi } from "vitest";
import { loadGeom, saveGeom, type PanelGeom } from "./panelGeometry";

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
