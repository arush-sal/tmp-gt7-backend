export type Bbox = { minX: number; minZ: number; maxX: number; maxZ: number };
export type View = { scale: number; offsetX: number; offsetZ: number };
export const FLIP_Z = false; // world Z -> screen Y, no flip

export function bboxOf(pts: ReadonlyArray<{ posX: number; posZ: number }>): Bbox | null {
  let minX = Infinity, minZ = Infinity, maxX = -Infinity, maxZ = -Infinity;
  for (const p of pts) {
    if (!Number.isFinite(p.posX) || !Number.isFinite(p.posZ)) continue;
    if (p.posX < minX) minX = p.posX; if (p.posX > maxX) maxX = p.posX;
    if (p.posZ < minZ) minZ = p.posZ; if (p.posZ > maxZ) maxZ = p.posZ;
  }
  return minX === Infinity ? null : { minX, minZ, maxX, maxZ };
}

export function unionBbox(a: Bbox | null, b: Bbox | null): Bbox | null {
  if (!a) return b; if (!b) return a;
  return { minX: Math.min(a.minX, b.minX), minZ: Math.min(a.minZ, b.minZ),
           maxX: Math.max(a.maxX, b.maxX), maxZ: Math.max(a.maxZ, b.maxZ) };
}

export function fitView(b: Bbox, w: number, h: number, pad: number, zoom: number, minExtentM = 50): View {
  const cx = (b.minX + b.maxX) / 2, cz = (b.minZ + b.maxZ) / 2;

  // Defensive clamps: fitView must always return a finite, strictly-positive
  // scale and finite offsets, no matter what garbage inputs it receives.
  const availW = Math.max(1, w - 2 * pad);
  const availH = Math.max(1, h - 2 * pad);
  const extentFloor = Math.max(minExtentM, 1);
  const spanX = Math.max(b.maxX - b.minX, extentFloor);
  const spanZ = Math.max(b.maxZ - b.minZ, extentFloor);
  const effectiveZoom = Number.isFinite(zoom) && zoom > 0 ? zoom : 1;

  let scale = Math.min(availW / spanX, availH / spanZ) * effectiveZoom;
  if (!Number.isFinite(scale) || scale <= 0) {
    scale = Math.min(availW / spanX, availH / spanZ);
    if (!Number.isFinite(scale) || scale <= 0) scale = 1;
  }

  // centre both axes: offset so the bbox centre maps to canvas centre
  return { scale, offsetX: cx - (w / 2) / scale, offsetZ: cz - (h / 2) / scale };
}

export function worldToScreen(v: View, x: number, z: number): { sx: number; sy: number } {
  return { sx: (x - v.offsetX) * v.scale, sy: (z - v.offsetZ) * v.scale };
}

// Inverse of worldToScreen: screen (pixel) coords -> world coords.
export function screenToWorld(v: View, sx: number, sy: number): { x: number; z: number } {
  return { x: sx / v.scale + v.offsetX, z: sy / v.scale + v.offsetZ };
}

export function bboxExceeds(inner: Bbox, outer: Bbox, factor: number): boolean {
  const gx = (outer.maxX - outer.minX) * factor, gz = (outer.maxZ - outer.minZ) * factor;
  return inner.minX < outer.minX - gx || inner.maxX > outer.maxX + gx ||
         inner.minZ < outer.minZ - gz || inner.maxZ > outer.maxZ + gz;
}
