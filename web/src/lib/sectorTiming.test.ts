import { clArcLengths, projectToClArc, sectorBoundaries, sectorIndexAt } from "./sectorTiming";

test("clArcLengths: cumulative arc-length, arc[0] = 0", () => {
  const cl: Array<[number, number]> = [[0, 0], [3, 4], [3, 10]];
  // seg1 = sqrt(3^2+4^2) = 5, seg2 = 6
  expect(clArcLengths(cl)).toEqual([0, 5, 11]);
});

test("clArcLengths: empty / single-point cl", () => {
  expect(clArcLengths([])).toEqual([]);
  expect(clArcLengths([[1, 1]])).toEqual([0]);
});

test("projectToClArc: nearest-vertex arc-length", () => {
  const cl: Array<[number, number]> = [[0, 0], [10, 0], [20, 0], [30, 0]];
  const arc = clArcLengths(cl);
  expect(projectToClArc(cl, arc, 0, 0)).toBe(0);
  expect(projectToClArc(cl, arc, 9, 0)).toBe(10); // nearer to vertex at x=10
  expect(projectToClArc(cl, arc, 22, 1)).toBe(20);
});

test("projectToClArc: empty cl -> NaN", () => {
  expect(Number.isNaN(projectToClArc([], [], 0, 0))).toBe(true);
});

test("sectorBoundaries: straight cl, labels on vertices -> midpoint boundaries", () => {
  const cl: Array<[number, number]> = [[0, 0], [10, 0], [20, 0], [30, 0]];
  const sectors = [
    { sector: "S1", x: 0, z: 0 },
    { sector: "S2", x: 10, z: 0 },
    { sector: "S3", x: 20, z: 0 },
  ];
  const result = sectorBoundaries(cl, sectors);
  expect(result).toEqual({ order: ["S1", "S2", "S3"], bounds: [5, 15] });
});

test("sectorBoundaries: fewer than 2 finite labels -> null", () => {
  const cl: Array<[number, number]> = [[0, 0], [10, 0]];
  expect(sectorBoundaries(cl, [{ sector: "S1", x: 0, z: 0 }])).toBeNull();
  expect(sectorBoundaries(cl, [])).toBeNull();
});

test("sectorBoundaries: empty cl -> null", () => {
  expect(sectorBoundaries([], [{ sector: "S1", x: 0, z: 0 }, { sector: "S2", x: 10, z: 0 }])).toBeNull();
});

test("sectorBoundaries: drops non-finite labels, preserves given order of the rest", () => {
  const cl: Array<[number, number]> = [[0, 0], [10, 0], [20, 0]];
  const sectors = [
    { sector: "S1", x: 0, z: 0 },
    { sector: "S2", x: NaN, z: 0 },
    { sector: "S3", x: 20, z: 0 },
  ];
  const result = sectorBoundaries(cl, sectors);
  expect(result).toEqual({ order: ["S1", "S3"], bounds: [10] });
});

test("sectorBoundaries: sectors given out of arc order -> sorted into monotonic boundaries + correct order", () => {
  const cl: Array<[number, number]> = [[0, 0], [10, 0], [20, 0], [30, 0]];
  // Deliberately out of arc order: S3 (arc 20) listed first, then S1 (arc 0), then S2 (arc 10).
  const sectors = [
    { sector: "S3", x: 20, z: 0 },
    { sector: "S1", x: 0, z: 0 },
    { sector: "S2", x: 10, z: 0 },
  ];
  const result = sectorBoundaries(cl, sectors);
  expect(result).toEqual({ order: ["S1", "S2", "S3"], bounds: [5, 15] });
  // boundaries are monotonic and sectorIndexAt resolves correctly regardless of input order.
  expect(result!.bounds[0]).toBeLessThan(result!.bounds[1]);
  expect(sectorIndexAt(0, result!.bounds)).toBe(0);
  expect(sectorIndexAt(4, result!.bounds)).toBe(0);
  expect(sectorIndexAt(12, result!.bounds)).toBe(1);
  expect(sectorIndexAt(20, result!.bounds)).toBe(2);
});

test("sectorIndexAt: index across ranges (2 boundaries -> 3 sectors)", () => {
  const bounds = [5, 15];
  expect(sectorIndexAt(0, bounds)).toBe(0);
  expect(sectorIndexAt(4.9, bounds)).toBe(0);
  expect(sectorIndexAt(5, bounds)).toBe(1);
  expect(sectorIndexAt(10, bounds)).toBe(1);
  expect(sectorIndexAt(14.9, bounds)).toBe(1);
  expect(sectorIndexAt(15, bounds)).toBe(2);
  expect(sectorIndexAt(30, bounds)).toBe(2);
});

test("sectorIndexAt: no boundaries -> always 0", () => {
  expect(sectorIndexAt(100, [])).toBe(0);
});
