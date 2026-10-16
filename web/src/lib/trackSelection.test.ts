import { initialSel, reduceAuto, reduceManual } from "./trackSelection";

test("auto-detect locks within the first observed lap", () => {
  let s = initialSel;
  s = reduceAuto(s, null, 0, false);          // pre-motion, no start lap yet
  expect(s.startLap).toBeNull();
  s = reduceAuto(s, null, 1, true);           // first moving sample -> window opens at lap 1
  expect(s.startLap).toBe(1);
  s = reduceAuto(s, "suzuka", 1, true);       // detect inside window
  expect(s).toMatchObject({ slug: "suzuka", locked: true });
});

test("auto-detect after the window is ignored", () => {
  let s = reduceAuto(initialSel, null, 1, true); // open at lap 1
  s = reduceAuto(s, null, 2, true);              // window closed (lap advanced, still unresolved)
  s = reduceAuto(s, "spa-francorchamps", 3, true);
  expect(s.slug).toBeNull(); // too late; stays trace-only until manual
});

test("closed window is a permanent latch; a regressed currentLap must not reopen it", () => {
  let s = reduceAuto(initialSel, null, 1, true); // open at lap 1
  s = reduceAuto(s, null, 2, true);              // window closes permanently
  expect(s).toMatchObject({ startLap: 1, autoClosed: true, locked: false, slug: null });
  s = reduceAuto(s, "spa-francorchamps", 1, true); // lap counter regressed to 1 (<= startLap)
  expect(s.slug).toBeNull();
  expect(s.locked).toBe(false);
});

test("manual pick always wins and is never overridden by later auto-detect", () => {
  let s = reduceManual(initialSel, "interlagos");
  expect(s).toMatchObject({ slug: "interlagos", locked: true, manual: true });
  s = reduceAuto(s, "suzuka", 1, true);
  expect(s.slug).toBe("interlagos");
});
