export type PanelGeom = { x: number; y: number; width: number; height: number };

const isGeom = (v: unknown): v is PanelGeom =>
  typeof v === "object" && v !== null &&
  ["x", "y", "width", "height"].every(k => typeof (v as Record<string, unknown>)[k] === "number");

export function loadGeom(key: string, fallback: PanelGeom): PanelGeom {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return fallback;
    const p = JSON.parse(raw);
    return isGeom(p) ? p : fallback;
  } catch {
    return fallback;
  }
}

export function saveGeom(key: string, geom: PanelGeom): void {
  try {
    localStorage.setItem(key, JSON.stringify(geom));
  } catch {
    /* storage disabled/full — non-fatal */
  }
}

// A restored/default geometry can land off-screen (react-rnd's `bounds` only constrains
// live drag/resize, not the initial `default`). Pull it back into the viewport before use.
export function clampGeom(g: PanelGeom, vw: number, vh: number, minW: number, minH: number): PanelGeom {
  const width = Math.min(Math.max(g.width, minW), vw);
  const height = Math.min(Math.max(g.height, minH), vh);
  const x = Math.min(Math.max(g.x, 0), Math.max(0, vw - width));
  const y = Math.min(Math.max(g.y, 0), Math.max(0, vh - 40)); // keep >=40px of the header grabbable
  return { x, y, width, height };
}
