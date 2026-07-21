import { lapSectors, type LapPoint } from "./lapSectors";
import type { DrawGeometry } from "./rkbGeometry";

// Straight-line cl, three sector labels at x=0,10,20 -> bounds at arc 5, 15 (see sectorTimer.test.ts).
const geometry: DrawGeometry = {
  left: [], right: [], outline: [], widthM: null, corners: [], startFinish: null,
  cl: [[0, 0], [10, 0], [20, 0], [30, 0]],
  sectors: [
    { label: "S1", x: 0, z: 0 },
    { label: "S2", x: 10, z: 0 },
    { label: "S3", x: 20, z: 0 },
  ],
};

test("null geometry -> null", () => {
  expect(lapSectors([], null)).toBeNull();
});

test("empty points -> initial all-null splits", () => {
  expect(lapSectors([], geometry)).toEqual([
    { sector: "S1", timeMs: null, deltaMs: null },
    { sector: "S2", timeMs: null, deltaMs: null },
    { sector: "S3", timeMs: null, deltaMs: null },
  ]);
});

test("replays a completed lap's points into final sector splits", () => {
  const points: LapPoint[] = [
    { posX: 0, posZ: 0, t: 0 },
    { posX: 9, posZ: 0, t: 2 },   // -> crosses into S2, S1 = 2000ms
    { posX: 19, posZ: 0, t: 5 },  // -> crosses into S3, S2 = 3000ms
    { posX: 29, posZ: 0, t: 7 },  // still in S3
  ];
  expect(lapSectors(points, geometry)).toEqual([
    { sector: "S1", timeMs: 2000, deltaMs: null },
    { sector: "S2", timeMs: 3000, deltaMs: null },
    { sector: "S3", timeMs: null, deltaMs: null },
  ]);
});

test("skips non-finite points", () => {
  const points: LapPoint[] = [
    { posX: 0, posZ: 0, t: 0 },
    { posX: NaN, posZ: 0, t: 1 },
    { posX: 9, posZ: 0, t: 2 },
  ];
  expect(lapSectors(points, geometry)).toEqual([
    { sector: "S1", timeMs: 2000, deltaMs: null },
    { sector: "S2", timeMs: null, deltaMs: null },
    { sector: "S3", timeMs: null, deltaMs: null },
  ]);
});
