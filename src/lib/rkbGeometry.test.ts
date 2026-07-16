import { toDrawGeometry, geometryBbox } from "./rkbGeometry";

test("toDrawGeometry drops null coords and absent sides", () => {
  const g = toDrawGeometry({
    corners: [{ turn: 1, apex_world: [10, 20], type: "tight" }, { turn: 2, apex_world: null }],
    sectors: [{ sector: "S1", label_world: [5, 5] }, { sector: "S2", label_world: null }],
    boundary: { source: "outline", outline: [[0, 0], [1, 1]] },
  });
  expect(g.corners).toEqual([{ turn: 1, x: 10, z: 20, type: "tight" }]);
  expect(g.sectors).toEqual([{ label: "S1", x: 5, z: 5 }]);
  expect(g.left).toEqual([]);
  expect(g.outline).toEqual([[0, 0], [1, 1]]);
  expect(g.startFinish).toBeNull();
});

test("start/finish only when the record carries it", () => {
  expect(toDrawGeometry({ start_finish: [3, 4] }).startFinish).toEqual([3, 4]);
  expect(toDrawGeometry({}).startFinish).toBeNull();
});

test("geometryBbox unions boundary + corners + sectors", () => {
  const g = toDrawGeometry({
    corners: [{ turn: 1, apex_world: [100, 0] }],
    boundary: { left: [[0, 0]], right: [[0, 50]] },
  });
  expect(geometryBbox(g)).toEqual({ minX: 0, minZ: 0, maxX: 100, maxZ: 50 });
});

test("empty record -> null bbox", () => {
  expect(geometryBbox(toDrawGeometry({}))).toBeNull();
});

test("extracts centerline + track width from boundary.cl / width.width_m", () => {
  const g = toDrawGeometry({
    boundary: { left: [[0, 0]], right: [[0, 50]], cl: [[0, 25], [100, 25]] },
    width: { width_m: 12.5 },
  });
  expect(g.cl).toEqual([[0, 25], [100, 25]]);
  expect(g.widthM).toBe(12.5);
});

test("cl absent / width_m absent or non-finite -> empty cl, null widthM", () => {
  expect(toDrawGeometry({}).cl).toEqual([]);
  expect(toDrawGeometry({}).widthM).toBeNull();
  expect(toDrawGeometry({ width: { width_m: NaN } }).widthM).toBeNull();
});

test("cl contributes to geometryBbox even when left/right are absent", () => {
  const g = toDrawGeometry({ boundary: { cl: [[10, 20], [200, 300]] } });
  expect(geometryBbox(g)).toEqual({ minX: 10, minZ: 20, maxX: 200, maxZ: 300 });
});

test("cl filters non-finite pairs", () => {
  const g = toDrawGeometry({ boundary: { cl: [[NaN, 0], [1, 1]] } });
  expect(g.cl).toEqual([[1, 1]]);
});

test("drops non-finite coords (needs-capture NaN world coords)", () => {
  const g = toDrawGeometry({
    corners: [{ turn: 1, apex_world: [NaN, 2] }, { turn: 2, apex_world: [10, 20] }],
    boundary: { outline: [[Infinity, 0], [1, 1]] },
    start_finish: [NaN, 4],
  });
  expect(g.corners).toEqual([{ turn: 2, x: 10, z: 20, type: undefined }]);
  expect(g.outline).toEqual([[1, 1]]);
  expect(g.startFinish).toBeNull();
});
