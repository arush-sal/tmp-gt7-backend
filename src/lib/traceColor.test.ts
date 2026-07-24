import { driveState, traceColor, colorBucket, brakeSpans, peakDeltaColor } from "./traceColor";

test("peakDeltaColor: + behind (red), - ahead (green), 0/null neutral", () => {
  expect(peakDeltaColor(null)).toBe("#9095a0");   // no reference -> neutral
  expect(peakDeltaColor(0)).toBe("#9095a0");       // exactly on -> neutral
  expect(peakDeltaColor(0.5e9)).toBe("#e10600");   // behind/slower -> red
  expect(peakDeltaColor(-1e9)).toBe("#2ed16a");    // ahead/faster -> green
});

test("driveState buckets", () => {
  expect(driveState(80, 0)).toBe("accel");
  expect(driveState(0, 0)).toBe("coast");
  expect(driveState(0, 60)).toBe("brake");
  expect(driveState(40, 40)).toBe("trail");
});

function rgbaChannels(css: string): [number, number, number] {
  const m = css.match(/^rgba\((\d+),(\d+),(\d+),/);
  if (!m) throw new Error(`not an rgba() string: ${css}`);
  return [Number(m[1]), Number(m[2]), Number(m[3])];
}

test("traceColor: coast is white (theme-independent, not black)", () => {
  const dark = traceColor(0, 0, "dark");
  const light = traceColor(0, 0, "light");
  expect(dark).toBe("rgba(255,255,255,1)");
  expect(light).toBe("rgba(255,255,255,1)");
  const [r, g, b] = rgbaChannels(dark);
  expect(r).toBe(255); expect(g).toBe(255); expect(b).toBe(255);
});

test("traceColor: full throttle is green-ish (red at full brake, for reference)", () => {
  const [r, g, b] = rgbaChannels(traceColor(100, 0, "dark"));
  expect(g).toBeGreaterThan(r);
  expect(g).toBeGreaterThan(b);
});

test("traceColor: full brake is red-ish", () => {
  const [r, g, b] = rgbaChannels(traceColor(0, 100, "dark"));
  expect(r).toBeGreaterThan(g);
  expect(r).toBeGreaterThan(b);
});

test("colorBucket changes when input bucket changes", () => {
  expect(colorBucket(0, 0)).toBe(colorBucket(3, 3));       // same ~16-bin bucket
  expect(colorBucket(0, 0)).not.toBe(colorBucket(100, 0)); // different bucket
});

test("brakeSpans applies hysteresis (no flicker on a dip)", () => {
  // enter at >=20, leave at <10: a dip to 15 stays inside the span
  const b = [0, 0, 30, 15, 30, 0, 0];
  expect(brakeSpans(b, 20, 10)).toEqual([[2, 4]]);
});

test("brakeSpans boundary cases: == onAt starts, == offAt stays, open-at-end closes", () => {
  // exact onAt (20) starts; exact offAt (10) stays inside (only < offAt closes); span open at end
  expect(brakeSpans([0, 20, 10, 25], 20, 10)).toEqual([[1, 3]]);
  // drops below offAt -> closes; never reopens
  expect(brakeSpans([20, 9, 0], 20, 10)).toEqual([[0, 0]]);
});

test("colorBucket stays in range 0..255 at the max corner", () => {
  const k = colorBucket(100, 100);
  expect(k).toBeGreaterThanOrEqual(0);
  expect(k).toBeLessThanOrEqual(255);
});

test("traceColor coast is not black in the light theme either", () => {
  expect(traceColor(0, 0, "light")).not.toBe("rgba(0,0,0,1)");
});
