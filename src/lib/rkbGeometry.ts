import { bboxOf, type Bbox } from "./trackTransform";

export type RkbRecord = {
  corners?: Array<{ turn: number; apex_world?: [number, number] | null; type?: string; direction?: string }>;
  sectors?: Array<{ sector: string; label_world?: [number, number] | null }>;
  boundary?: { source?: string; left?: Array<[number, number]>; right?: Array<[number, number]>; outline?: Array<[number, number]>; cl?: Array<[number, number]> };
  width?: { width_m?: number };
  confidence?: { coordinate_usability?: string; physical_boundary_fidelity?: string };
  start_finish?: [number, number] | null;
};

export type DrawGeometry = {
  left: Array<[number, number]>; right: Array<[number, number]>; outline: Array<[number, number]>;
  cl: Array<[number, number]>; widthM: number | null;
  corners: Array<{ turn: number; x: number; z: number; type?: string }>;
  sectors: Array<{ label: string; x: number; z: number }>;
  startFinish: [number, number] | null;
};

const finitePair = (p?: [number, number] | null): p is [number, number] =>
  !!p && Number.isFinite(p[0]) && Number.isFinite(p[1]);

export function toDrawGeometry(rec: RkbRecord): DrawGeometry {
  const b = rec.boundary ?? {};
  const widthM = typeof rec.width?.width_m === "number" && Number.isFinite(rec.width.width_m)
    ? rec.width.width_m
    : null;
  return {
    left: (b.left ?? []).filter(finitePair),
    right: (b.right ?? []).filter(finitePair),
    outline: (b.outline ?? []).filter(finitePair),
    cl: (b.cl ?? []).filter(finitePair),
    widthM,
    corners: (rec.corners ?? []).filter(c => finitePair(c.apex_world))
      .map(c => ({ turn: c.turn, x: c.apex_world![0], z: c.apex_world![1], type: c.type })),
    sectors: (rec.sectors ?? []).filter(s => finitePair(s.label_world))
      .map(s => ({ label: s.sector, x: s.label_world![0], z: s.label_world![1] })),
    startFinish: finitePair(rec.start_finish) ? rec.start_finish : null,
  };
}

export function geometryBbox(g: DrawGeometry): Bbox | null {
  const pts: Array<{ posX: number; posZ: number }> = [];
  for (const s of [g.left, g.right, g.outline, g.cl]) for (const [x, z] of s) pts.push({ posX: x, posZ: z });
  for (const c of g.corners) pts.push({ posX: c.x, posZ: c.z });
  for (const s of g.sectors) pts.push({ posX: s.x, posZ: s.z });
  if (g.startFinish) pts.push({ posX: g.startFinish[0], posZ: g.startFinish[1] });
  return bboxOf(pts);
}
