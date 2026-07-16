import { SectorTimer } from "./sectorTimer";
import type { DrawGeometry } from "./rkbGeometry";

// Straight-line cl, three sector labels at x=0,10,20 -> bounds at arc 5, 15 (see sectorTiming.test.ts).
const geometry: DrawGeometry = {
  left: [], right: [], outline: [], widthM: null, corners: [], startFinish: null,
  cl: [[0, 0], [10, 0], [20, 0], [30, 0]],
  sectors: [
    { label: "S1", x: 0, z: 0 },
    { label: "S2", x: 10, z: 0 },
    { label: "S3", x: 20, z: 0 },
  ],
};

test("no geometry -> always returns null", () => {
  const t = new SectorTimer(null);
  expect(t.update({ sessionKey: "s", lap: 1, lapTimeNs: 0, x: 0, z: 0 })).toBeNull();
});

test("fewer than 2 usable sector labels -> null", () => {
  const g: DrawGeometry = { ...geometry, sectors: [{ label: "S1", x: 0, z: 0 }] };
  const t = new SectorTimer(g);
  expect(t.update({ sessionKey: "s", lap: 1, lapTimeNs: 0, x: 0, z: 0 })).toBeNull();
});

test("records a split when crossing into the next sector", () => {
  const t = new SectorTimer(geometry);
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 0, x: 0, z: 0 }); // sector 0
  const r1 = t.update({ sessionKey: "s", lap: 1, lapTimeNs: 2_000_000_000, x: 9, z: 0 }); // crosses into sector 1 at arc 10
  expect(r1).toEqual([
    { sector: "S1", timeMs: 2000 },
    { sector: "S2", timeMs: null },
    { sector: "S3", timeMs: null },
  ]);
});

test("ignores a jump of more than one sector (jitter guard)", () => {
  const t = new SectorTimer(geometry);
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 0, x: 0, z: 0 }); // sector 0
  const r = t.update({ sessionKey: "s", lap: 1, lapTimeNs: 5_000_000_000, x: 22, z: 1 }); // arc ~20 -> sector 2, skips sector 1
  expect(r).toEqual([
    { sector: "S1", timeMs: null },
    { sector: "S2", timeMs: null },
    { sector: "S3", timeMs: null },
  ]);
});

test("records successive splits across all sectors then finalizes on lap rollover", () => {
  const t = new SectorTimer(geometry);
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 0, x: 0, z: 0 });
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 2_000_000_000, x: 9, z: 0 });   // -> sector 1, S1 = 2000ms
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 5_000_000_000, x: 19, z: 0 });  // -> sector 2, S2 = 3000ms
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 7_000_000_000, x: 29, z: 0 });  // still sector 2, last known lap time before wrap
  const onRollover = t.update({ sessionKey: "s", lap: 2, lapTimeNs: 10_000_000, x: 0, z: 0 }); // lap rolls over
  expect(onRollover).toEqual([
    { sector: "S1", timeMs: 2000 },
    { sector: "S2", timeMs: 3000 },
    { sector: "S3", timeMs: 2000 }, // 7000ms - 5000ms entry
  ]);
  // next call builds a fresh lap
  const nextLap = t.update({ sessionKey: "s", lap: 2, lapTimeNs: 20_000_000, x: 0, z: 0 });
  expect(nextLap).toEqual([
    { sector: "S1", timeMs: null },
    { sector: "S2", timeMs: null },
    { sector: "S3", timeMs: null },
  ]);
});

test("session change resets state even mid-lap", () => {
  const t = new SectorTimer(geometry);
  t.update({ sessionKey: "s1", lap: 1, lapTimeNs: 0, x: 0, z: 0 });
  t.update({ sessionKey: "s1", lap: 1, lapTimeNs: 2_000_000_000, x: 9, z: 0 }); // S1 = 2000ms
  const r = t.update({ sessionKey: "s2", lap: 1, lapTimeNs: 1_000_000, x: 0, z: 0 }); // new session
  expect(r).toEqual([
    { sector: "S1", timeMs: null },
    { sector: "S2", timeMs: null },
    { sector: "S3", timeMs: null },
  ]);
});
