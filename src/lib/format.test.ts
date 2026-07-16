import { describe, it, expect } from "vitest";
import { parseDuration, nsToClock, nsToDeltaSeconds, msToSectorTime, PLACEHOLDER } from "./format";

describe("parseDuration", () => {
  it("parses whole seconds", () => expect(parseDuration("77s")).toBe(77_000_000_000));
  it("parses fractional seconds to ns", () => expect(parseDuration("77.210s")).toBe(77_210_000_000));
  it("parses full 9-digit precision", () =>
    expect(parseDuration("1.000340012s")).toBe(1_000_340_012));
  it("parses zero", () => expect(parseDuration("0s")).toBe(0));
  it("throws on missing trailing s", () => expect(() => parseDuration("77.21")).toThrow());
  it("throws on non-numeric", () => expect(() => parseDuration("abc")).toThrow());
  it("throws on >9 fractional digits", () => expect(() => parseDuration("1.1234567890s")).toThrow());
  it("throws on negative", () => expect(() => parseDuration("-1s")).toThrow());
  it("throws when the parsed ns exceeds safe-integer range", () =>
    expect(() => parseDuration("9007199255s")).toThrow(/safe/));
  it("accepts exactly Number.MAX_SAFE_INTEGER ns", () =>
    expect(parseDuration("9007199.254740991s")).toBe(Number.MAX_SAFE_INTEGER));
  it("throws one ns past MAX_SAFE_INTEGER", () =>
    expect(() => parseDuration("9007199.254740992s")).toThrow(/safe/));
  it("zero-pads a 1-digit fraction to ns", () =>
    expect(parseDuration("1.1s")).toBe(1_100_000_000));
});

describe("nsToClock", () => {
  it("formats to M:SS.mmm", () => expect(nsToClock(77_210_000_000)).toBe("1:17.210"));
  it("carries across the minute boundary on round-up", () =>
    expect(nsToClock(59_999_500_000)).toBe("1:00.000"));
  it("does not carry just below the boundary", () =>
    expect(nsToClock(59_999_499_999)).toBe("0:59.999"));
});

describe("nsToDeltaSeconds", () => {
  it("formats positive with + and s", () => expect(nsToDeltaSeconds(910_000_000)).toBe("+0.910s"));
  it("formats negative with Unicode minus", () =>
    expect(nsToDeltaSeconds(-910_000_000)).toBe("−0.910s"));
  it("treats zero as positive", () => expect(nsToDeltaSeconds(0)).toBe("+0.000s"));
});

describe("msToSectorTime", () => {
  it("formats sub-minute as SS.mmm", () => expect(msToSectorTime(12345)).toBe("12.345"));
  it("formats a minute or more as M:SS.mmm", () => expect(msToSectorTime(65_432)).toBe("1:05.432"));
  it("null -> PLACEHOLDER", () => expect(msToSectorTime(null)).toBe(PLACEHOLDER));
});

describe("PLACEHOLDER", () => {
  it("is the em dash", () => expect(PLACEHOLDER).toBe("—"));
});
