export type DriveState = "accel" | "coast" | "brake" | "trail";

const FLOOR = { dark: [139, 148, 158], light: [75, 85, 99] }; // #8b949e / #4b5563
const GREEN = [22, 163, 74]; // #16a34a -- darker/more saturated so full-throttle reads clearly on the light-grey band
const RED = [225, 6, 0];      // #e10600
const AMBER = [245, 158, 11]; // #f59e0b -- coast (off-throttle, off-brake)
const COAST_MIX = 0.55;       // how strongly coast leans into amber vs. the theme floor

export function driveState(throttle: number, brake: number): DriveState {
  const t = throttle > 10, b = brake > 10;
  if (t && b) return "trail";
  if (b) return "brake";
  if (t) return "accel";
  return "coast";
}

export function traceColor(throttle: number, brake: number, theme: "light" | "dark", alpha = 1): string {
  const f = FLOOR[theme];

  if (driveState(throttle, brake) === "coast") {
    const r = Math.round(f[0] + (AMBER[0] - f[0]) * COAST_MIX);
    const g = Math.round(f[1] + (AMBER[1] - f[1]) * COAST_MIX);
    const b = Math.round(f[2] + (AMBER[2] - f[2]) * COAST_MIX);
    return `rgba(${r},${g},${b},${alpha})`;
  }

  const th = Math.max(0, Math.min(1, throttle / 100));
  const br = Math.max(0, Math.min(1, brake / 100));
  const mix = Math.max(th, br);
  const target = th >= br ? GREEN : RED; // dominant input picks hue; both-high reads amber via blend
  const amber = th > 0 && br > 0;
  const tgt = amber ? [(GREEN[0] + RED[0]) / 2, (GREEN[1] + RED[1]) / 2, (GREEN[2] + RED[2]) / 2] : target;
  const r = Math.round(f[0] + (tgt[0] - f[0]) * mix);
  const g = Math.round(f[1] + (tgt[1] - f[1]) * mix);
  const b = Math.round(f[2] + (tgt[2] - f[2]) * mix);
  return `rgba(${r},${g},${b},${alpha})`;
}

export function colorBucket(throttle: number, brake: number): number {
  const t = Math.min(15, Math.floor(throttle / 6.25)); // 0..15
  const b = Math.min(15, Math.floor(brake / 6.25));     // 0..15
  return t * 16 + b;                                     // 0..255
}

export function brakeSpans(brake: ReadonlyArray<number>, onAt = 12, offAt = 6): Array<[number, number]> {
  const spans: Array<[number, number]> = [];
  let start = -1;
  for (let i = 0; i < brake.length; i++) {
    if (start < 0) { if (brake[i] >= onAt) start = i; }
    else if (brake[i] < offAt) { spans.push([start, i - 1]); start = -1; }
  }
  if (start >= 0) spans.push([start, brake.length - 1]);
  return spans;
}
