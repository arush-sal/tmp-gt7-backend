import { bboxOf, unionBbox, fitView, worldToScreen, screenToWorld, bboxExceeds, FLIP_Z } from "./trackTransform";

test("FLIP_Z is false (world Z maps to screen Y, no flip)", () => {
  expect(FLIP_Z).toBe(false);
  const v = fitView({ minX: 0, minZ: 0, maxX: 100, maxZ: 100 }, 200, 200, 0, 1);
  const lo = worldToScreen(v, 0, 0), hi = worldToScreen(v, 0, 100);
  expect(hi.sy).toBeGreaterThan(lo.sy); // larger Z -> larger Y (no flip)
});

test("fitView is uniform and aspect-preserving", () => {
  const v = fitView({ minX: 0, minZ: 0, maxX: 200, maxZ: 100 }, 400, 400, 0, 1);
  const a = worldToScreen(v, 0, 0), b = worldToScreen(v, 200, 0), c = worldToScreen(v, 0, 100);
  const sxPerM = (b.sx - a.sx) / 200, syPerM = (c.sy - a.sy) / 100;
  expect(sxPerM).toBeCloseTo(syPerM, 6); // same scale both axes
});

test("degenerate bbox uses the min-extent floor (no divide-by-zero)", () => {
  const v = fitView({ minX: 5, minZ: 5, maxX: 5, maxZ: 5 }, 200, 200, 0, 1, 50);
  expect(Number.isFinite(v.scale)).toBe(true);
  expect(v.scale).toBeGreaterThan(0);
});

test("bboxOf and unionBbox", () => {
  expect(bboxOf([])).toBeNull();
  expect(bboxOf([{ posX: 1, posZ: 2 }, { posX: 3, posZ: -1 }]))
    .toEqual({ minX: 1, minZ: -1, maxX: 3, maxZ: 2 });
  expect(unionBbox({ minX: 0, minZ: 0, maxX: 1, maxZ: 1 }, { minX: -1, minZ: 2, maxX: 0, maxZ: 3 }))
    .toEqual({ minX: -1, minZ: 0, maxX: 1, maxZ: 3 });
});

test("fitView guards against zoom <= 0 (no divide-by-zero -> Infinity/NaN)", () => {
  const v = fitView({ minX: 0, minZ: 0, maxX: 200, maxZ: 100 }, 400, 400, 0, 0);
  expect(Number.isFinite(v.scale)).toBe(true);
  expect(v.scale).toBeGreaterThan(0);
  expect(Number.isFinite(v.offsetX)).toBe(true);
  expect(Number.isFinite(v.offsetZ)).toBe(true);
});

test("fitView guards against non-finite zoom", () => {
  const v = fitView({ minX: 0, minZ: 0, maxX: 200, maxZ: 100 }, 400, 400, 0, NaN);
  expect(Number.isFinite(v.scale)).toBe(true);
  expect(v.scale).toBeGreaterThan(0);
  expect(Number.isFinite(v.offsetX)).toBe(true);
  expect(Number.isFinite(v.offsetZ)).toBe(true);
});

test("fitView guards against negative zoom (no silent axis flip)", () => {
  const v = fitView({ minX: 0, minZ: 0, maxX: 200, maxZ: 100 }, 400, 400, 0, -1);
  expect(Number.isFinite(v.scale)).toBe(true);
  expect(v.scale).toBeGreaterThan(0);
  expect(Number.isFinite(v.offsetX)).toBe(true);
  expect(Number.isFinite(v.offsetZ)).toBe(true);
});

test("fitView guards against pad >= half the canvas (non-positive drawable area)", () => {
  const v = fitView({ minX: 0, minZ: 0, maxX: 200, maxZ: 100 }, 400, 400, 250, 1);
  expect(Number.isFinite(v.scale)).toBe(true);
  expect(v.scale).toBeGreaterThan(0);
  expect(Number.isFinite(v.offsetX)).toBe(true);
  expect(Number.isFinite(v.offsetZ)).toBe(true);
});

test("fitView guards against zero/negative drawable height too", () => {
  const v = fitView({ minX: 0, minZ: 0, maxX: 200, maxZ: 100 }, 400, 400, 200, 1);
  expect(Number.isFinite(v.scale)).toBe(true);
  expect(v.scale).toBeGreaterThan(0);
  expect(Number.isFinite(v.offsetX)).toBe(true);
  expect(Number.isFinite(v.offsetZ)).toBe(true);
});

test("fitView guards against minExtentM <= 0 with a degenerate bbox (no divide-by-zero)", () => {
  const v = fitView({ minX: 5, minZ: 5, maxX: 5, maxZ: 5 }, 200, 200, 0, 1, 0);
  expect(Number.isFinite(v.scale)).toBe(true);
  expect(v.scale).toBeGreaterThan(0);
  expect(Number.isFinite(v.offsetX)).toBe(true);
  expect(Number.isFinite(v.offsetZ)).toBe(true);
});

test("fitView guards against negative minExtentM with a degenerate bbox", () => {
  const v = fitView({ minX: 5, minZ: 5, maxX: 5, maxZ: 5 }, 200, 200, 0, 1, -10);
  expect(Number.isFinite(v.scale)).toBe(true);
  expect(v.scale).toBeGreaterThan(0);
  expect(Number.isFinite(v.offsetX)).toBe(true);
  expect(Number.isFinite(v.offsetZ)).toBe(true);
});

test("screenToWorld is the inverse of worldToScreen (round-trip)", () => {
  const v = fitView({ minX: 0, minZ: 0, maxX: 200, maxZ: 100 }, 400, 400, 10, 2);
  const cases: Array<[number, number]> = [[0, 0], [123.4, -56.7], [-10, 200]];
  for (const [x, z] of cases) {
    const { sx, sy } = worldToScreen(v, x, z);
    const back = screenToWorld(v, sx, sy);
    expect(back.x).toBeCloseTo(x, 6);
    expect(back.z).toBeCloseTo(z, 6);
  }
});

test("bboxExceeds detects a trace spilling past the track bbox", () => {
  const track = { minX: 0, minZ: 0, maxX: 100, maxZ: 100 };
  expect(bboxExceeds({ minX: 10, minZ: 10, maxX: 90, maxZ: 90 }, track, 0.1)).toBe(false);
  expect(bboxExceeds({ minX: -50, minZ: 0, maxX: 100, maxZ: 100 }, track, 0.1)).toBe(true);
});
