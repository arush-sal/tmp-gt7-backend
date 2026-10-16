// @vitest-environment jsdom
import { describe, it, expect, beforeEach, vi } from "vitest";
import { loadVersioned, saveVersioned, SCHEMA_VERSION } from "./persistState";

describe("persistState", () => {
  beforeEach(() => localStorage.clear());

  it("round-trips a payload through save then load", () => {
    saveVersioned("k", { a: 1 });
    expect(loadVersioned<{ a: number }>("k")).toEqual({ a: 1 });
  });

  it("returns null when schemaVersion doesn't match", () => {
    localStorage.setItem("k", JSON.stringify({ schemaVersion: SCHEMA_VERSION + 1, payload: { a: 1 } }));
    expect(loadVersioned("k")).toBeNull();
  });

  it("returns null on malformed JSON", () => {
    localStorage.setItem("k", "{not json");
    expect(loadVersioned("k")).toBeNull();
  });

  it("returns null when the key is absent", () => {
    expect(loadVersioned("missing")).toBeNull();
  });

  it("swallows a throwing setItem", () => {
    const spy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("QuotaExceededError");
    });
    expect(() => saveVersioned("k", { a: 1 })).not.toThrow();
    spy.mockRestore();
  });

  it("returns null for an old/unversioned blob (discard-on-upgrade)", () => {
    localStorage.setItem("k", JSON.stringify({ foo: 1 }));
    expect(loadVersioned("k")).toBeNull();
  });

  it("returns null when getItem throws", () => {
    const spy = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("SecurityError");
    });
    expect(loadVersioned("k")).toBeNull();
    spy.mockRestore();
  });
});
