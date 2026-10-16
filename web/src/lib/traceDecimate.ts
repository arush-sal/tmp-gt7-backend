import type { TracePoint } from "./historyStore";
import { worldToScreen, type View } from "./trackTransform";
import { colorBucket } from "./traceColor";

export function surfaceOff(surface: string): boolean {
  return surface.length > 0 && [...surface].some(c => c !== "T");
}
export function surfaceHas(surface: string, code: string): boolean {
  return surface.includes(code);
}

function isFinitePoint(p: TracePoint): boolean {
  return Number.isFinite(p.posX) && Number.isFinite(p.posZ);
}

export function decimate(pts: ReadonlyArray<TracePoint>, v: View, minPx = 1): number[] {
  if (pts.length === 0) return [];
  const firstFiniteIdx = pts.findIndex(isFinitePoint);
  if (firstFiniteIdx === -1) return [];
  // Key "always keep last" off the last FINITE index, not pts.length-1: a trailing
  // non-finite tail must not drop the newest valid sample (the live car position).
  let lastFiniteIdx = firstFiniteIdx;
  for (let j = pts.length - 1; j > firstFiniteIdx; j--) {
    if (isFinitePoint(pts[j])) { lastFiniteIdx = j; break; }
  }
  const keep = [firstFiniteIdx];
  let last = worldToScreen(v, pts[firstFiniteIdx].posX, pts[firstFiniteIdx].posZ);
  let lastBucket = colorBucket(pts[firstFiniteIdx].throttle, pts[firstFiniteIdx].brake);
  let lastSurface = pts[firstFiniteIdx].surface;
  for (let i = firstFiniteIdx + 1; i < pts.length; i++) {
    const p = pts[i];
    if (!isFinitePoint(p)) continue;
    const bucket = colorBucket(p.throttle, p.brake);
    const s = worldToScreen(v, p.posX, p.posZ);
    const moved = Math.hypot(s.sx - last.sx, s.sy - last.sy) >= minPx;
    const transition = bucket !== lastBucket || p.surface !== lastSurface;
    const isLast = i === lastFiniteIdx;
    if (moved || transition || isLast) {
      keep.push(i); last = s; lastBucket = bucket; lastSurface = p.surface;
    }
  }
  return keep;
}

export function penUpBreaks(
  pts: ReadonlyArray<TracePoint>, dts: ReadonlyArray<number>, maxSpeedMps = 200,
): Set<number> {
  const breaks = new Set<number>();
  const MARGIN = 4; // allow 4x plausible per-frame distance before calling it a teleport
  for (let i = 1; i < pts.length; i++) {
    if (!isFinitePoint(pts[i]) || !isFinitePoint(pts[i - 1])) {
      breaks.add(i);
      continue;
    }
    const d = Math.hypot(pts[i].posX - pts[i - 1].posX, pts[i].posZ - pts[i - 1].posZ);
    const raw = dts[i];
    const dt = Number.isFinite(raw) ? Math.max(raw, 1 / 60) : 1 / 60;
    if (d > maxSpeedMps * dt * MARGIN) breaks.add(i);
  }
  return breaks;
}
