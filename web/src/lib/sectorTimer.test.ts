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
    { sector: "S1", timeMs: 2000, deltaMs: null }, // first recording -> no baseline delta
    { sector: "S2", timeMs: null, deltaMs: null },
    { sector: "S3", timeMs: null, deltaMs: null },
  ]);
});

test("ignores a jump of more than one sector (jitter guard)", () => {
  const t = new SectorTimer(geometry);
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 0, x: 0, z: 0 }); // sector 0
  const r = t.update({ sessionKey: "s", lap: 1, lapTimeNs: 5_000_000_000, x: 22, z: 1 }); // arc ~20 -> sector 2, skips sector 1
  expect(r).toEqual([
    { sector: "S1", timeMs: null, deltaMs: null },
    { sector: "S2", timeMs: null, deltaMs: null },
    { sector: "S3", timeMs: null, deltaMs: null },
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
    { sector: "S1", timeMs: 2000, deltaMs: null },
    { sector: "S2", timeMs: 3000, deltaMs: null },
    { sector: "S3", timeMs: 2000, deltaMs: null }, // 7000ms - 5000ms entry
  ]);
  // Sticky: the fresh lap has recorded nothing yet, so the previous lap's sector times
  // are retained (NOT cleared) until each sector completes again.
  const nextLap = t.update({ sessionKey: "s", lap: 2, lapTimeNs: 20_000_000, x: 0, z: 0 });
  expect(nextLap).toEqual([
    { sector: "S1", timeMs: 2000, deltaMs: null },
    { sector: "S2", timeMs: 3000, deltaMs: null },
    { sector: "S3", timeMs: 2000, deltaMs: null },
  ]);
});

test("lap 2 overwrites each sector with a lap-over-lap delta (- faster, + slower)", () => {
  const t = new SectorTimer(geometry);
  // Lap 1: S1 = 2000ms, S2 = 3000ms.
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 0, x: 0, z: 0 });
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 2_000_000_000, x: 9, z: 0 });  // -> S1 = 2000
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 5_000_000_000, x: 19, z: 0 }); // -> S2 = 3000
  t.update({ sessionKey: "s", lap: 1, lapTimeNs: 7_000_000_000, x: 29, z: 0 });
  t.update({ sessionKey: "s", lap: 2, lapTimeNs: 10_000_000, x: 0, z: 0 });     // rollover -> S3 = 2000

  // Lap 2: S1 completes 200ms faster (1800), S2 200ms slower (3200).
  t.update({ sessionKey: "s", lap: 2, lapTimeNs: 1_800_000_000, x: 9, z: 0 });  // -> S1 = 1800, delta -200
  const r = t.update({ sessionKey: "s", lap: 2, lapTimeNs: 5_000_000_000, x: 19, z: 0 }); // -> S2 = 3200, delta +200
  expect(r).toEqual([
    { sector: "S1", timeMs: 1800, deltaMs: -200 }, // faster -> negative (blue)
    { sector: "S2", timeMs: 3200, deltaMs: 200 },  // slower -> positive (red)
    { sector: "S3", timeMs: 2000, deltaMs: null }, // not yet re-recorded this lap -> sticky, no new delta
  ]);
});

test("seedState restores sticky times and survives the first same-session update", () => {
  const t = new SectorTimer(geometry);
  t.seedState("s", 4, [
    { sector: "S1", timeMs: 2000, deltaMs: -100 },
    { sector: "S2", timeMs: 3000, deltaMs: null },
    { sector: "S3", timeMs: 2500, deltaMs: 50 },
  ]);
  // First live frame of the SAME session at the start of a lap: must NOT wipe the seed
  // (no session reset, and a zero rollover split is ignored by the finalize guard).
  const r = t.update({ sessionKey: "s", lap: 5, lapTimeNs: 10_000_000, x: 0, z: 0 });
  expect(r).toEqual([
    { sector: "S1", timeMs: 2000, deltaMs: -100 },
    { sector: "S2", timeMs: 3000, deltaMs: null },
    { sector: "S3", timeMs: 2500, deltaMs: 50 },
  ]);
});

test("a different incoming session still wipes a seeded state", () => {
  const t = new SectorTimer(geometry);
  t.seedState("old", 4, [
    { sector: "S1", timeMs: 2000, deltaMs: null },
    { sector: "S2", timeMs: 3000, deltaMs: null },
    { sector: "S3", timeMs: 2500, deltaMs: null },
  ]);
  const r = t.update({ sessionKey: "new", lap: 1, lapTimeNs: 0, x: 0, z: 0 });
  expect(r).toEqual([
    { sector: "S1", timeMs: null, deltaMs: null },
    { sector: "S2", timeMs: null, deltaMs: null },
    { sector: "S3", timeMs: null, deltaMs: null },
  ]);
});

test("session change resets state even mid-lap", () => {
  const t = new SectorTimer(geometry);
  t.update({ sessionKey: "s1", lap: 1, lapTimeNs: 0, x: 0, z: 0 });
  t.update({ sessionKey: "s1", lap: 1, lapTimeNs: 2_000_000_000, x: 9, z: 0 }); // S1 = 2000ms
  const r = t.update({ sessionKey: "s2", lap: 1, lapTimeNs: 1_000_000, x: 0, z: 0 }); // new session
  expect(r).toEqual([
    { sector: "S1", timeMs: null, deltaMs: null },
    { sector: "S2", timeMs: null, deltaMs: null },
    { sector: "S3", timeMs: null, deltaMs: null },
  ]);
});
