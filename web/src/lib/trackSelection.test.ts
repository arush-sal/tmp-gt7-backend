import { initialSel, reduceAuto, reduceManual, type SelState } from "./trackSelection";

test("auto-detect locks within the first observed lap", () => {
  let s = initialSel;
  s = reduceAuto(s, null, 0, false);          // pre-motion, no start lap yet
  expect(s.startLap).toBeNull();
  s = reduceAuto(s, null, 1, true);           // first moving sample -> window opens at lap 1
  expect(s.startLap).toBe(1);
  s = reduceAuto(s, "suzuka", 1, true);       // detect inside window
  expect(s).toMatchObject({ slug: "suzuka", locked: true });
});

test("a detection after the window closed still fills an EMPTY selection", () => {
  // The backend may only resolve after a full lap; an empty selection must not stay "no track
  // detected" for the whole session just because the first lap ended first.
  let s = reduceAuto(initialSel, null, 1, true); // open at lap 1
  s = reduceAuto(s, null, 2, true);              // window closed (lap advanced, still unresolved)
  expect(s).toMatchObject({ startLap: 1, autoClosed: true, locked: false, slug: null });
  s = reduceAuto(s, "spa-francorchamps", 3, true);
  expect(s).toMatchObject({ slug: "spa-francorchamps", locked: true });
});

test("after the window closed, a late provisional guess is followed until it locks", () => {
  let s = reduceAuto(reduceAuto(initialSel, null, 1, true), null, 2, true);
  s = reduceAuto(s, "sardegna-road-b", 2, true, true);
  expect(s).toMatchObject({ slug: "sardegna-road-b", provisional: true, locked: false });
  s = reduceAuto(s, "sardegna-road-a", 2, true, false);
  expect(s).toMatchObject({ slug: "sardegna-road-a", provisional: false, locked: true });
});

test("a closed window never replaces a slug that is already shown (e.g. restored from storage)", () => {
  let s: SelState = { ...initialSel, slug: "saved-track" };
  s = reduceAuto(s, null, 1, true);
  s = reduceAuto(s, null, 2, true);               // window closes with the saved slug shown
  s = reduceAuto(s, "spa-francorchamps", 3, true);
  expect(s.slug).toBe("saved-track");
});

test("following a provisional guess returns the same state object when nothing changed", () => {
  let s = reduceAuto(reduceAuto(initialSel, null, 1, true), "suzuka", 1, true, true);
  expect(reduceAuto(s, "suzuka", 1, true, true)).toBe(s);
  s = reduceAuto(s, null, 1, true);
  expect(reduceAuto(s, null, 1, true)).toBe(s);
});

test("manual pick always wins and is never overridden by later auto-detect", () => {
  let s = reduceManual(initialSel, "interlagos");
  expect(s).toMatchObject({ slug: "interlagos", locked: true, manual: true });
  s = reduceAuto(s, "suzuka", 1, true);
  expect(s.slug).toBe("interlagos");
});

test("a provisional detection is followed (switches) until the backend locks it", () => {
  let s = reduceAuto(initialSel, null, 1, true);           // window opens at lap 1
  s = reduceAuto(s, "sardegna-road-b", 1, true, true);       // provisional guess
  expect(s).toMatchObject({ slug: "sardegna-road-b", locked: false, provisional: true });
  s = reduceAuto(s, "sardegna-road-a", 1, true, true);       // backend switched its guess
  expect(s).toMatchObject({ slug: "sardegna-road-a", locked: false, provisional: true });
  s = reduceAuto(s, "sardegna-road-a", 2, true, true);       // lap rolled over: still following, not closed
  expect(s).toMatchObject({ slug: "sardegna-road-a", provisional: true, autoClosed: false });
  s = reduceAuto(s, "sardegna-road-a2", 2, true, false);     // locked confident (even after lap 1)
  expect(s).toMatchObject({ slug: "sardegna-road-a2", locked: true, provisional: false });
  s = reduceAuto(s, "suzuka", 2, true, true);                // nothing moves a lock
  expect(s.slug).toBe("sardegna-road-a2");
});

test("a withdrawn provisional guess is dropped, and later guesses are still followed", () => {
  let s = reduceAuto(initialSel, null, 1, true);
  s = reduceAuto(s, "interlagos", 1, true, true);
  s = reduceAuto(s, null, 1, true);                          // backend withdrew the guess
  expect(s).toMatchObject({ slug: null, provisional: true, locked: false });
  s = reduceAuto(s, null, 2, true);                          // lap rolled over while undecided
  s = reduceAuto(s, "dragon-trail-gardens", 2, true, false); // a later lock is still taken
  expect(s).toMatchObject({ slug: "dragon-trail-gardens", locked: true });
});

test("manual pick overrides a provisional guess and is never replaced", () => {
  let s = reduceAuto(reduceAuto(initialSel, null, 1, true), "interlagos", 1, true, true);
  s = reduceManual(s, "suzuka");
  expect(s).toMatchObject({ slug: "suzuka", manual: true, provisional: false });
  s = reduceAuto(s, "interlagos", 1, true, false);
  expect(s.slug).toBe("suzuka");
});
