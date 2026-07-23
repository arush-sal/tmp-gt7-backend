import { describe, expect, it } from "vitest";
import { mergeSticky } from "./stickyView";
import type { LiveView } from "./liveView";
import { PLACEHOLDER } from "./format";

const base: LiveView = {
  status: "live",
  speed: "214",
  gear: "5",
  rpm: "7200",
  lap: "2/5",
  position: "P1/8",
  fuelPct: "80%",
  currentLapTime: "1:02.500",
  delta: "+0.500s",
  predicted: "1:55.100",
  bestLap: "1:56.931",
  lastLap: "1:57.842",
  tyreTemps: {
    fl: { value: "84°C", tone: "optimal" },
    fr: { value: "85°C", tone: "optimal" },
    rl: { value: "80°C", tone: "optimal" },
    rr: { value: "81°C", tone: "optimal" },
  },
  deltaTone: "blue",
  fuelTone: "green",
  steering: "+6.9°",
  surface: "TTTT",
  carCategory: "GRN",
};

function placeholderView(): LiveView {
  return {
    status: "idle",
    speed: PLACEHOLDER,
    gear: PLACEHOLDER,
    rpm: PLACEHOLDER,
    lap: PLACEHOLDER,
    position: PLACEHOLDER,
    fuelPct: PLACEHOLDER,
    currentLapTime: PLACEHOLDER,
    delta: PLACEHOLDER,
    predicted: PLACEHOLDER,
    bestLap: PLACEHOLDER,
    lastLap: PLACEHOLDER,
    tyreTemps: {
      fl: { value: PLACEHOLDER, tone: "none" },
      fr: { value: PLACEHOLDER, tone: "none" },
      rl: { value: PLACEHOLDER, tone: "none" },
      rr: { value: PLACEHOLDER, tone: "none" },
    },
    deltaTone: "none",
    fuelTone: "none",
    steering: PLACEHOLDER,
    surface: PLACEHOLDER,
    carCategory: PLACEHOLDER,
  };
}

describe("mergeSticky", () => {
  it("prev null -> returns next + live", () => {
    const result = mergeSticky(null, base, true);
    expect(result).toEqual({ ...base, live: true });
  });

  it("next has a real speed -> speed = next's; live passed through", () => {
    const prev = { ...base, live: true };
    const next = { ...base, speed: "200" };
    const result = mergeSticky(prev, next, false);
    expect(result.speed).toBe("200");
    expect(result.live).toBe(false);
  });

  it("next.speed placeholder, prev.speed real -> keeps prev's", () => {
    const prev = { ...base, speed: "214", live: true };
    const next = { ...placeholderView(), speed: PLACEHOLDER };
    const result = mergeSticky(prev, next, false);
    expect(result.speed).toBe("214");
  });

  it("tyreTemps: next fl placeholder, prev fl real -> keep prev fl (value + tone)", () => {
    const prev = { ...base, live: true };
    const next = { ...base, tyreTemps: { ...base.tyreTemps, fl: { value: PLACEHOLDER, tone: "none" as const } } };
    const result = mergeSticky(prev, next, true);
    expect(result.tyreTemps.fl).toEqual({ value: "84°C", tone: "optimal" });
    expect(result.tyreTemps.fr).toEqual(base.tyreTemps.fr);
  });

  it("delta kept from prev when next delta is placeholder -> deltaTone also from prev", () => {
    const prev = { ...base, delta: "+0.500s", deltaTone: "blue" as const, live: true };
    const next = { ...base, delta: PLACEHOLDER, deltaTone: "none" as const };
    const result = mergeSticky(prev, next, true);
    expect(result.delta).toBe("+0.500s");
    expect(result.deltaTone).toBe("blue");
  });

  it("live=false threads through", () => {
    const prev = { ...base, live: true };
    const result = mergeSticky(prev, base, false);
    expect(result.live).toBe(false);
  });

  it("multiple fields placeholder-and-kept simultaneously -> each kept from prev, tones carried", () => {
    const prev = {
      ...base,
      delta: "+0.500s", deltaTone: "blue" as const,
      fuelPct: "80%", fuelTone: "green" as const,
      tyreTemps: { ...base.tyreTemps, fl: { value: "84°C", tone: "optimal" as const } },
      live: true,
    };
    const next = {
      ...base,
      delta: PLACEHOLDER, deltaTone: "none" as const,
      fuelPct: PLACEHOLDER, fuelTone: "none" as const,
      tyreTemps: { ...base.tyreTemps, fl: { value: PLACEHOLDER, tone: "none" as const } },
    };
    const result = mergeSticky(prev, next, true);
    expect(result.delta).toBe("+0.500s");
    expect(result.fuelPct).toBe("80%");
    expect(result.tyreTemps.fl).toEqual({ value: "84°C", tone: "optimal" });
    expect(result.deltaTone).toBe(prev.deltaTone);
    expect(result.fuelTone).toBe(prev.fuelTone);
  });

  it("status always taken from next", () => {
    const prev = { ...base, status: "live" as const, live: true };
    const next = { ...base, status: "idle" as const };
    const result = mergeSticky(prev, next, false);
    expect(result.status).toBe("idle");
  });
});
