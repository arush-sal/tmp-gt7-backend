// Pure helpers for the P2 lap-analysis scrub feature: locating the nearest sample to a
// clicked world point, and clamping a scrub index into a lap's sample range.

export function nearestSampleIndex(
  pts: ReadonlyArray<{ posX: number; posZ: number }>,
  worldX: number,
  worldZ: number,
): number {
  let best = -1;
  let bestDist = Infinity;
  for (let i = 0; i < pts.length; i++) {
    const p = pts[i];
    if (!Number.isFinite(p.posX) || !Number.isFinite(p.posZ)) continue;
    const dx = p.posX - worldX, dz = p.posZ - worldZ;
    const dist = dx * dx + dz * dz;
    if (dist < bestDist) { bestDist = dist; best = i; }
  }
  return best;
}

export function clampIndex(i: number, len: number): number {
  if (len <= 0) return 0;
  if (i < 0) return 0;
  if (i > len - 1) return len - 1;
  return i;
}
