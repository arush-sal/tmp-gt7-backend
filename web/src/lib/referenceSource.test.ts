// @vitest-environment jsdom
import { describe, it, expect, beforeEach } from "vitest";
import { loadRefSource, saveRefSource } from "./referenceSource";

beforeEach(() => localStorage.clear());

describe("loadRefSource", () => {
  it("defaults to auto when nothing or junk is saved", () => {
    expect(loadRefSource()).toBe("auto");
    localStorage.setItem("gt7:ref-source", "bogus");
    expect(loadRefSource()).toBe("auto");
  });
  it("round-trips every explicit choice", () => {
    for (const s of ["auto", "stored", "predicted"] as const) { saveRefSource(s); expect(loadRefSource()).toBe(s); }
  });
});
