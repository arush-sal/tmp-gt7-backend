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
