import { describe, it, expect } from "vitest";
import { UNKNOWN_TRACK, trackKey, analysisTracks, reconcileAnalysis, defaultAnalysisLap, fastestLapNumber } from "./analysisSelection";

const s = (id: string, track_slug: string | null) => ({ id, track_slug });

describe("analysisSelection", () => {
  it("trackKey maps a missing track to the Unknown key", () => {
    expect(trackKey(s("a", "spa"))).toBe("spa");
    expect(trackKey(s("a", null))).toBe(UNKNOWN_TRACK);
  });

  it("analysisTracks lists only tracks with sessions (named), plus Unknown last when present", () => {
    const names = { spa: "Spa", suzuka: "Suzuka", monza: "Monza" };
    expect(analysisTracks([s("a", "suzuka"), s("b", null), s("c", "spa"), s("d", "suzuka")], names)).toEqual([
      { slug: "spa", name: "Spa" }, { slug: "suzuka", name: "Suzuka" }, { slug: UNKNOWN_TRACK, name: "Unknown track" },
    ]);
    expect(analysisTracks([s("a", "x")], {})).toEqual([{ slug: "x", name: "x" }]);
  });

  // sessions arrive newest first (useSessions sorts them)
  const list = [s("new", "spa"), s("mid", "suzuka"), s("old", null)];
  it("defaults to the most recent session and its track", () => {
    expect(reconcileAnalysis(list, null, null)).toEqual({ id: "new", track: "spa" });
  });
  it("a picked session sets the track", () => {
    expect(reconcileAnalysis(list, "old", null)).toEqual({ id: "old", track: UNKNOWN_TRACK });
  });
  it("a picked track whose sessions exclude the current one selects that track's most recent session", () => {
    expect(reconcileAnalysis(list, "new", "suzuka")).toEqual({ id: "mid", track: "suzuka" });
  });
  it("consistent selection is unchanged", () => {
    expect(reconcileAnalysis(list, "mid", "suzuka")).toEqual({ id: "mid", track: "suzuka" });
  });
  it("no sessions -> nothing", () => {
    expect(reconcileAnalysis([], null, null)).toEqual({ id: null, track: null });
  });

  const lap = (n: number, sec: number, over: object = {}) => ({ lap_number: n, duration_ns: sec * 1e9, complete: true, valid: true, ...over });
  it("defaultAnalysisLap: the fastest valid complete lap, ignoring a partial end fragment", () => {
    expect(defaultAnalysisLap([lap(1, 92), lap(2, 90), lap(3, 91), lap(5, 0.198, { complete: false, valid: false })], 0)).toBe(2);
  });
  it("defaultAnalysisLap: ignores formation (0) and cooldown (> total) laps", () => {
    expect(defaultAnalysisLap([lap(0, 60), lap(1, 92), lap(2, 91), lap(3, 50)], 2)).toBe(2);
  });
  it("defaultAnalysisLap: no valid lap -> the last complete lap -> All laps", () => {
    expect(defaultAnalysisLap([lap(1, 92, { valid: false }), lap(2, 95, { valid: false }), lap(3, 1, { complete: false })], 0)).toBe(2);
    expect(defaultAnalysisLap([lap(1, 1, { complete: false, valid: false })], 0)).toBe("all");
    expect(defaultAnalysisLap([], 0)).toBe("all");
  });
  it("fastestLapNumber: shortest complete valid racing lap; a partial fragment never wins", () => {
    expect(fastestLapNumber([lap(1, 92), lap(2, 90), lap(5, 0.198, { complete: false, valid: false })], 0)).toBe(2);
    expect(fastestLapNumber([lap(1, 92, { valid: false }), lap(2, 1, { complete: false })], 0)).toBeNull();
    expect(fastestLapNumber([lap(0, 60), lap(1, 92), lap(3, 50)], 2)).toBe(1);
  });
});
