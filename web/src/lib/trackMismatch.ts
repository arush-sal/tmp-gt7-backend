import type { TracePoint } from "./historyStore";
import { geometryBbox, type DrawGeometry } from "./rkbGeometry";

// LENIENT bbox-envelope heuristic: this is a dev nudge, not an authoritative
// track-switch detector. A sustained off-track excursion or a spin can trip
// it (false positive), and a wrong-but-overlapping track's bbox can hide a
// real mismatch (false negative). Never auto-switches anything.

export interface InsideFractionResult {
  fraction: number;
  moving: number;
}

function isFinitePoint(p: TracePoint): boolean {
  return Number.isFinite(p.posX) && Number.isFinite(p.posZ);
}

export function insideFraction(
  pts: ReadonlyArray<TracePoint>,
  g: DrawGeometry,
  tolM = 15,
): InsideFractionResult {
  const b = geometryBbox(g);
  if (!b) return { fraction: 1, moving: 0 }; // no geometry to compare against -> do not flag

  let moving = 0;
  let inside = 0;
  for (let i = 1; i < pts.length; i++) {
    const prev = pts[i - 1];
    const cur = pts[i];
    if (!isFinitePoint(prev) || !isFinitePoint(cur)) continue; // skip non-finite segments

    const d = Math.hypot(cur.posX - prev.posX, cur.posZ - prev.posZ);
    if (d < 0.05) continue; // stationary sample, ignore

    moving++;
    if (cur.posX >= b.minX - tolM && cur.posX <= b.maxX + tolM &&
        cur.posZ >= b.minZ - tolM && cur.posZ <= b.maxZ + tolM) inside++;
  }

  return { fraction: moving === 0 ? 1 : inside / moving, moving };
}

export function isMismatch(
  res: InsideFractionResult,
  minSamples = 300,
  minFraction = 0.6,
): boolean {
  return res.moving >= minSamples && res.fraction < minFraction;
}
