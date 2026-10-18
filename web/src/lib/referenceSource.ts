/** "auto" picks whichever of stored/predicted is available and faster (see useSelectedReference). */
export type RefSource = "auto" | "stored" | "predicted";
const KEY = "gt7:ref-source";

/** loadRefSource returns the saved choice; "auto" by default (and for unknown values). */
export function loadRefSource(): RefSource {
  try {
    const v = localStorage.getItem(KEY);
    return v === "stored" || v === "predicted" ? v : "auto";
  } catch { return "auto"; }
}
/** saveRefSource persists the choice; failures are non-fatal. */
export function saveRefSource(s: RefSource): void {
  try { localStorage.setItem(KEY, s); } catch { /* no storage */ }
}
