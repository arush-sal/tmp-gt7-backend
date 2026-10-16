import { insideFraction, isMismatch } from "./trackMismatch";
import { toDrawGeometry } from "./rkbGeometry";

const g = toDrawGeometry({ boundary: { outline: [[0, 0], [100, 0], [100, 100], [0, 100]] } });
const P = (posX: number, posZ: number) => ({ posX, posZ, throttle: 50, brake: 0, surface: "TTTT" });
const NP = () => ({ posX: NaN, posZ: NaN, throttle: 50, brake: 0, surface: "TTTT" });

test("insideFraction ~1 when the trace sits within the track envelope", () => {
  expect(insideFraction([P(10, 10), P(50, 50), P(90, 90)], g, 5).fraction).toBeCloseTo(1, 5);
});

test("insideFraction drops when the trace is far outside", () => {
  expect(insideFraction([P(500, 500), P(600, 600)], g, 5).fraction).toBeLessThan(0.5);
});

test("no usable geometry -> fraction 1, moving 0 (never false-trips)", () => {
  const res = insideFraction([P(9, 9)], toDrawGeometry({}), 5);
  expect(res.fraction).toBe(1);
  expect(res.moving).toBe(0);
});

test("NaN current point is skipped", () => {
  const res = insideFraction([P(10, 10), NP(), P(50, 50)], g, 5);
  // moving should only count the finite-to-finite transition(s); NaN segments skipped
  expect(Number.isFinite(res.fraction)).toBe(true);
  expect(res.moving).toBeLessThanOrEqual(1);
});

test("NaN previous point is skipped", () => {
  const res = insideFraction([NP(), P(10, 10), P(50, 50)], g, 5);
  expect(Number.isFinite(res.fraction)).toBe(true);
});

test("sustained all-NaN run yields fraction 1, moving 0, isMismatch false", () => {
  const pts = Array.from({ length: 500 }, () => NP());
  const res = insideFraction(pts, g, 5);
  expect(res).toEqual({ fraction: 1, moving: 0 });
  expect(isMismatch(res)).toBe(false);
});

test("clearly-outside finite trace yields low fraction and isMismatch true only past minSamples", () => {
  const few = Array.from({ length: 50 }, (_, i) => P(500 + i, 500 + i));
  const resFew = insideFraction(few, g, 5);
  expect(isMismatch(resFew, 300, 0.6)).toBe(false); // too few samples

  const many = Array.from({ length: 400 }, (_, i) => P(500 + i, 500 + i));
  const resMany = insideFraction(many, g, 5);
  expect(resMany.fraction).toBeLessThan(0.6);
  expect(isMismatch(resMany, 300, 0.6)).toBe(true);
});

test("isMismatch needs sustained samples AND low fraction", () => {
  expect(isMismatch({ fraction: 0.1, moving: 500 }, 300, 0.6)).toBe(true);
  expect(isMismatch({ fraction: 0.1, moving: 50 }, 300, 0.6)).toBe(false); // too few samples
  expect(isMismatch({ fraction: 0.9, moving: 500 }, 300, 0.6)).toBe(false); // aligned enough
});
