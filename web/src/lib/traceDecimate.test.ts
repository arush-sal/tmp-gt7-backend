import { decimate, penUpBreaks, surfaceOff, surfaceHas } from "./traceDecimate";

const V = { scale: 1, offsetX: 0, offsetZ: 0 };
const P = (posX: number, posZ: number, throttle = 50, brake = 0, surface = "TTTT") =>
  ({ posX, posZ, throttle, brake, surface });

test("surface helpers (any-wheel rule)", () => {
  expect(surfaceOff("TTTT")).toBe(false);
  expect(surfaceOff("TTCT")).toBe(true);
  expect(surfaceHas("TGTT", "G")).toBe(true);
  expect(surfaceHas("TTTT", "C")).toBe(false);
});

test("decimate drops sub-pixel moves but keeps endpoints", () => {
  const pts = [P(0, 0), P(0.2, 0), P(0.4, 0), P(50, 0)];
  const keep = decimate(pts, V, 1);
  expect(keep[0]).toBe(0);
  expect(keep[keep.length - 1]).toBe(3);
  expect(keep).not.toContain(1); // <1px move dropped
});

test("decimate always keeps a colour-bucket change even if sub-pixel", () => {
  const pts = [P(0, 0, 0, 0), P(0.2, 0, 100, 0)]; // tiny move but bucket flips
  expect(decimate(pts, V, 1)).toContain(1);
});

test("decimate always keeps a surface-code change", () => {
  const pts = [P(0, 0, 50, 0, "TTTT"), P(0.2, 0, 50, 0, "TTGG")];
  expect(decimate(pts, V, 1)).toContain(1);
});

test("penUpBreaks flags a teleport but not normal 60Hz motion", () => {
  const pts = [P(0, 0), P(1, 0), P(600, 0)]; // ~600m jump
  const dts = [0, 1 / 60, 1 / 60];
  const breaks = penUpBreaks(pts, dts, 200);  // 200 m/s * 1/60 ~= 3.3m plausible
  expect(breaks.has(2)).toBe(true);
  expect(breaks.has(1)).toBe(false);
});

test("penUpBreaks flags a teleport even when that sample's dt is NaN", () => {
  const pts = [P(0, 0), P(1, 0), P(600, 0)]; // ~600m jump
  const dts = [0, 1 / 60, NaN];
  const breaks = penUpBreaks(pts, dts, 200);
  expect(breaks.has(2)).toBe(true);
});

test("penUpBreaks adds a break when a segment endpoint coordinate is NaN", () => {
  const pts = [P(0, 0), P(NaN, 0), P(1, 0)];
  const dts = [0, 1 / 60, 1 / 60];
  const breaks = penUpBreaks(pts, dts, 200);
  expect(breaks.has(1)).toBe(true); // (0,0) -> (NaN,0)
  expect(breaks.has(2)).toBe(true); // (NaN,0) -> (1,0)
});

test("decimate omits non-finite points, including a non-finite first point", () => {
  const pts = [P(NaN, 0), P(0, 0), P(0.2, 0), P(50, 0)];
  const keep = decimate(pts, V, 1);
  for (const i of keep) {
    expect(Number.isFinite(pts[i].posX)).toBe(true);
    expect(Number.isFinite(pts[i].posZ)).toBe(true);
  }
  expect(keep).not.toContain(0);
  expect(keep[keep.length - 1]).toBe(3);
});

test("decimate keeps the last FINITE point even with a non-finite trailing tail", () => {
  // index 2 is the last finite point but only a sub-pixel move from index 1; index 3 is NaN.
  const pts = [P(0, 0), P(50, 0), P(50.2, 0), P(NaN, 0)];
  const keep = decimate(pts, V, 1);
  expect(keep).toContain(2);          // last finite sample kept despite sub-pixel move
  expect(keep).not.toContain(3);      // non-finite tail excluded
});
