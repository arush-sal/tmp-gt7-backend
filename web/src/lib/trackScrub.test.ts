import { nearestSampleIndex, clampIndex } from "./trackScrub";

test("nearestSampleIndex finds the closest point by Euclidean distance", () => {
  const pts = [{ posX: 0, posZ: 0 }, { posX: 10, posZ: 0 }, { posX: 20, posZ: 0 }];
  expect(nearestSampleIndex(pts, 9, 1)).toBe(1);
  expect(nearestSampleIndex(pts, 0.4, 0)).toBe(0);
  expect(nearestSampleIndex(pts, 19, 0)).toBe(2);
});

test("nearestSampleIndex skips non-finite points", () => {
  const pts = [{ posX: NaN, posZ: NaN }, { posX: 10, posZ: 0 }];
  expect(nearestSampleIndex(pts, 10, 0)).toBe(1);
});

test("nearestSampleIndex returns -1 when there are no finite points", () => {
  const pts = [{ posX: NaN, posZ: 0 }, { posX: 0, posZ: NaN }];
  expect(nearestSampleIndex(pts, 0, 0)).toBe(-1);
});

test("nearestSampleIndex returns -1 for an empty array", () => {
  expect(nearestSampleIndex([], 0, 0)).toBe(-1);
});

test("clampIndex clamps to [0, len-1]", () => {
  expect(clampIndex(5, 10)).toBe(5);
  expect(clampIndex(-1, 10)).toBe(0);
  expect(clampIndex(20, 10)).toBe(9);
});

test("clampIndex handles len <= 0 by returning 0", () => {
  expect(clampIndex(5, 0)).toBe(0);
  expect(clampIndex(-3, -1)).toBe(0);
});
