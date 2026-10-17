export type DriveState = "accel" | "coast" | "brake" | "trail";

// Solid, saturated colours per drive state -- NOT an intensity gradient. An intensity
// gradient blended toward a grey "floor", so partial throttle rendered as a muddy dark-green;
// the line now reads categorically (any throttle = green, brake = red, coast = white (dark) / grey (light), trail-
// brake = amber), matching the legend. Precise magnitudes live in the telemetry graphs.
const GREEN = "46,209,106";  // #2ed16a -- throttle
const RED = "240,22,14";     // brake
const AMBER = "250,180,20";  // trail-braking (throttle + brake together)

export function driveState(throttle: number, brake: number): DriveState {
  const t = throttle > 10, b = brake > 10;
  if (t && b) return "trail";
  if (b) return "brake";
  if (t) return "accel";
  return "coast";
}

// Only "coast" is theme-aware: white vanishes on the light canvas (there is no track band behind
// the line), so light theme uses a mid-dark neutral grey. The saturated states read on both.
const COAST = { dark: "255,255,255", light: "92,98,108" } as const; // light = #5c626c

export function traceColor(throttle: number, brake: number, theme: "light" | "dark", alpha = 1): string {
  switch (driveState(throttle, brake)) {
    case "coast": return `rgba(${COAST[theme]},${alpha})`;
    case "brake": return `rgba(${RED},${alpha})`;
    case "trail": return `rgba(${AMBER},${alpha})`;
    default:      return `rgba(${GREEN},${alpha})`; // accel
  }
}

export function colorBucket(throttle: number, brake: number): number {
  const t = Math.min(15, Math.floor(throttle / 6.25)); // 0..15
  const b = Math.min(15, Math.floor(brake / 6.25));     // 0..15
  return t * 16 + b;                                     // 0..255
}

// Speed peak/valley time-delta colouring. Sign: `+` (delta > 0) = this lap is behind/slower at that
// point (time lost) -> red; `-` = ahead/faster (time gained) -> green. null/0 -> neutral grey.
export function peakDeltaColor(deltaNs: number | null): string {
  if (deltaNs == null || deltaNs === 0) return "#9095a0"; // neutral
  return deltaNs > 0 ? "#e10600" : "#2ed16a"; // + behind (red) / - ahead (green)
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
