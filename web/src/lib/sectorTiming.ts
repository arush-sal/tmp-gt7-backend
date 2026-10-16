/**
 * sectorTiming.ts — approximate distance-based sector partition + live split timing.
 *
 * MODEL (confirmed): sectors are contiguous distance-based divisions of the lap, not
 * corner-based. The backend only gives us a per-sector label anchor (mid-sector point,
 * world metres) and the track centerline polyline. We derive approximate sector
 * boundaries as the arc-length midpoints between consecutive sector labels, projected
 * onto the centerline (nearest-vertex, not segment-interpolated). This is a Voronoi-ish
 * partition on arc-length, not the real sector-line geometry — label it "approx" in the UI.
 */

export type SectorLabel = { sector: string; x: number; z: number };

export type SectorPartition = { order: string[]; bounds: number[] };

/** Cumulative arc-length at each cl vertex. arc[0] = 0. Empty input -> empty output. */
export function clArcLengths(cl: ReadonlyArray<[number, number]>): number[] {
  if (cl.length === 0) return [];
  const arc: number[] = [0];
  for (let i = 1; i < cl.length; i++) {
    const [x0, z0] = cl[i - 1];
    const [x1, z1] = cl[i];
    const seg = Math.hypot(x1 - x0, z1 - z0);
    arc.push(arc[i - 1] + seg);
  }
  return arc;
}

/**
 * Arc-length of the cl vertex nearest to (x, z). Nearest-vertex only (no segment
 * interpolation) — good enough for a sector-timing approximation. Empty cl -> NaN.
 */
export function projectToClArc(
  cl: ReadonlyArray<[number, number]>,
  arc: ReadonlyArray<number>,
  x: number,
  z: number,
): number {
  if (cl.length === 0) return NaN;
  let bestIdx = 0;
  let bestDist = Infinity;
  for (let i = 0; i < cl.length; i++) {
    const [cx, cz] = cl[i];
    const d = Math.hypot(cx - x, cz - z);
    if (d < bestDist) { bestDist = d; bestIdx = i; }
  }
  return arc[bestIdx];
}

/**
 * Projects each sector label onto the cl arc-length axis, SORTS by that projected
 * arc-length ascending (labels are not guaranteed to arrive in arc order -- e.g. a
 * figure-8 track, or odd label placement -- and midpoints between out-of-order entries
 * would not be monotonic), then derives interior boundaries as the midpoint arc-length
 * between each consecutive (sorted) pair. Drops sectors with a non-finite label or a
 * label that fails to project (NaN arc-length). Returns null when fewer than 2 usable
 * sectors remain, or cl is empty.
 */
export function sectorBoundaries(
  cl: ReadonlyArray<[number, number]>,
  sectors: ReadonlyArray<SectorLabel>,
): SectorPartition | null {
  if (cl.length === 0) return null;
  const finite = sectors.filter(s => Number.isFinite(s.x) && Number.isFinite(s.z));
  if (finite.length < 2) return null;

  const arc = clArcLengths(cl);
  const projected = finite
    .map(s => ({ sector: s.sector, arcLen: projectToClArc(cl, arc, s.x, s.z) }))
    .filter(p => !Number.isNaN(p.arcLen));
  if (projected.length < 2) return null;

  projected.sort((a, b) => a.arcLen - b.arcLen);

  const bounds: number[] = [];
  for (let i = 1; i < projected.length; i++) {
    bounds.push((projected[i - 1].arcLen + projected[i].arcLen) / 2);
  }
  return { order: projected.map(p => p.sector), bounds };
}

/**
 * Index of the sector a given arc-length falls in: 0 for arcLen < bounds[0], k for
 * bounds[k-1] <= arcLen < bounds[k], and bounds.length (last sector) beyond the final
 * boundary. No boundaries -> always 0.
 */
export function sectorIndexAt(arcLen: number, bounds: ReadonlyArray<number>): number {
  let idx = 0;
  while (idx < bounds.length && arcLen >= bounds[idx]) idx++;
  return idx;
}
