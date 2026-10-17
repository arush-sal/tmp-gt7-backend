export type RefSource = "stored" | "predicted";
const KEY = "gt7:ref-source";

/** loadRefSource returns the saved choice; "stored" by default. */
export function loadRefSource(): RefSource {
  try { return localStorage.getItem(KEY) === "predicted" ? "predicted" : "stored"; } catch { return "stored"; }
}
/** saveRefSource persists the choice; failures are non-fatal. */
export function saveRefSource(s: RefSource): void {
  try { localStorage.setItem(KEY, s); } catch { /* no storage */ }
}
