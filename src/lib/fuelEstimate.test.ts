import { estimateLapsLeft } from "./fuelEstimate";

test("normal case: sane burn rate -> laps left", () => {
  expect(estimateLapsLeft(50, 10)).toBe(5);
});

test("null inputs -> null", () => {
  expect(estimateLapsLeft(null, 10)).toBeNull();
  expect(estimateLapsLeft(50, null)).toBeNull();
  expect(estimateLapsLeft(null, null)).toBeNull();
});

test("usedPerLap below floor (tiny/noisy) -> null, not an absurd laps-left", () => {
  // e.g. 50 / 0.24 would be ~208 laps -- implausible, and the underlying rate is noise.
  expect(estimateLapsLeft(50, 0.24)).toBeNull();
});

test("usedPerLap at/above floor -> computes even if the result is a bit high", () => {
  expect(estimateLapsLeft(50, 0.5)).toBe(100);
});

test("usedPerLap zero or negative (refuel/rounding blip) -> null", () => {
  expect(estimateLapsLeft(50, 0)).toBeNull();
  expect(estimateLapsLeft(50, -1)).toBeNull();
});

test("projected value beyond the plausibility cap -> null", () => {
  // 100% tank, 0.51%/lap burn rate -> ~196 laps, under cap; bump burn down further to breach it.
  expect(estimateLapsLeft(100, 0.49)).toBeNull(); // below floor anyway
  expect(estimateLapsLeft(1000, 0.5)).toBeNull(); // absurd fuel_pct edge case, way over cap
});
