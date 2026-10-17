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

/** DOCK_WIDTH_KEY is the localStorage key for the user's preferred dock width. */
export const DOCK_WIDTH_KEY = "gt7:telemetry-width";
/** DOCK_WIDTH_DEFAULT is the dock width used when no valid preference is stored. */
export const DOCK_WIDTH_DEFAULT = 380;
/** DOCK_WIDTH_MIN is the narrowest the dock can be resized to. */
export const DOCK_WIDTH_MIN = 280;
/** TRACK_PANEL_MIN is the minimum width the track panel keeps beside the dock. */
export const TRACK_PANEL_MIN = 320;
/** CENTER_GAP is the flex gap of .center-column (must match index.css). */
export const CENTER_GAP = 10;

/** dockMaxWidth is the widest the dock may be given the measured .center-column width; never below the minimum. */
export const dockMaxWidth = (available: number): number =>
  Math.max(DOCK_WIDTH_MIN, available - TRACK_PANEL_MIN - CENTER_GAP);

/** clampDockWidth clamps a width to [DOCK_WIDTH_MIN, dockMaxWidth(available)]. */
export const clampDockWidth = (w: number, available: number): number =>
  Math.min(Math.max(w, DOCK_WIDTH_MIN), dockMaxWidth(available));

/** loadDockWidth returns the stored width preference (not clamped to available space), default when missing/invalid. */
export function loadDockWidth(): number {
  try {
    const n = Number(localStorage.getItem(DOCK_WIDTH_KEY));
    return Math.max(DOCK_WIDTH_MIN, Number.isFinite(n) && n > 0 ? n : DOCK_WIDTH_DEFAULT);
  } catch {
    return DOCK_WIDTH_DEFAULT;
  }
}

/** saveDockWidth persists the dock width preference (rounded); failures are non-fatal. */
export function saveDockWidth(w: number): void {
  try {
    localStorage.setItem(DOCK_WIDTH_KEY, String(Math.round(w)));
  } catch {
    /* storage disabled/full — non-fatal */
  }
}
