import { describe, it, expect, beforeEach } from "vitest";
import { putLap, getLap, resetLapCache } from "./lapCache";
import type { LapSample } from "./lapView";

const s = (t: number): LapSample => ({
  t, posX: 0, posZ: 0, surface: "", distance: 0, throttle: 0, brake: 0, clutch: 0, rpm: 0,
  gear: 0, speed: 0, boost: 0, delta: 0, tyreFL: 0, tyreFR: 0, tyreRL: 0, tyreRR: 0, fuel: 0, steering: 0,
});

beforeEach(resetLapCache);

describe("lapCache", () => {
  it("put/get by (session, lap); misses are undefined", () => {
    putLap("a", 1, [s(1)]);
    expect(getLap("a", 1)).toEqual([s(1)]);
    expect(getLap("a", 2)).toBeUndefined();
    expect(getLap("b", 1)).toBeUndefined();
  });
  it("resetLapCache clears everything", () => {
    putLap("a", 1, [s(1)]);
    resetLapCache();
    expect(getLap("a", 1)).toBeUndefined();
  });
});
