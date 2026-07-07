/**
 * format.ts — pure presentation transforms
 *
 * The contract comment says: "the frontend only applies pure presentation
 * transforms (ms -> '1:57.842', standing -> colour)." This file is that layer.
 *
 * These are already implemented so the UI renders correctly. Read through them
 * to understand the patterns — you'll write similar utility functions as the
 * project grows.
 */

/**
 * Converts a lap or sector time in milliseconds to "M:SS.mmm" display format.
 *
 * e.g. 117842 → "1:57.842"
 *
 * The arithmetic:
 *   minutes = floor(ms / 60000)          — whole minutes
 *   seconds = floor((ms % 60000) / 1000) — remaining whole seconds
 *   millis  = ms % 1000                  — remaining milliseconds
 *
 * String.padStart(width, char) zero-pads a string to the given width.
 * It's the TS equivalent of fmt.Sprintf("%02d", n) in Go.
 */
export function formatLapTime(ms: number): string {
  const minutes = Math.floor(ms / 60_000);
  const seconds = Math.floor((ms % 60_000) / 1_000);
  const millis  = ms % 1_000;
  return `${minutes}:${String(seconds).padStart(2, "0")}.${String(millis).padStart(3, "0")}`;
}

/**
 * Formats a delta in milliseconds as "+0.122" or "−0.054".
 *
 * Note the Unicode minus sign (−, U+2212) vs a hyphen (-). The Unicode minus
 * is wider and aligns better in a monospace column next to the + sign.
 */
export function formatDelta(ms: number): string {
  const sign = ms >= 0 ? "+" : "−";
  const abs  = Math.abs(ms);
  return `${sign}${(abs / 1_000).toFixed(3)}`;
}

/**
 * Formats a delta already in seconds (e.g. corner analysis values).
 * e.g. 0.321 → "+0.321", -0.054 → "−0.054"
 */
export function formatDeltaSecs(s: number): string {
  const sign = s >= 0 ? "+" : "−";
  return `${sign}${Math.abs(s).toFixed(3)}`;
}

/**
 * Returns the CSS colour-role for a millisecond delta.
 *
 * Convention (matches the wireframe spec):
 *   positive ms = lost time  → red
 *   negative ms = gained time → green
 *   within threshold         → amber (neutral)
 */
export function deltaRoleMs(ms: number, thresholdMs = 50): "red" | "green" | "amber" {
  if (Math.abs(ms) <= thresholdMs) return "amber";
  return ms > 0 ? "red" : "green";
}

/**
 * Same but for second-valued deltas (corner analysis).
 */
export function deltaRoleSecs(s: number, thresholdSecs = 0.05): "red" | "green" | "amber" {
  if (Math.abs(s) <= thresholdSecs) return "amber";
  return s > 0 ? "red" : "green";
}

/**
 * Maps a Standing value (or undefined) to the CSS class that the UI uses.
 * gridFastest → purple pill, personalBest → blue pill, absent → plain mono.
 */
export function standingClass(standing: string | undefined): string {
  if (standing === "gridFastest") return "pill-purple";
  if (standing === "personalBest") return "pill-blue";
  return "mono";
}

/** Placeholder rendered wherever a live/enrichment value is not yet available. */
export const PLACEHOLDER = "—"; // em dash —

/**
 * Parses a protobuf-JSON Duration string ("77.210s", "0s", "1.000340012s") to integer
 * nanoseconds. Rejects negatives and >9 fractional digits. Throws on any malformed input.
 * Shared by validateLive (accept/reject) and liveView (display) so there is one parser.
 */
export function parseDuration(s: string): number {
  const m = /^(\d+)(?:\.(\d{1,9}))?s$/.exec(s);
  if (!m) throw new Error(`invalid duration string: ${JSON.stringify(s)}`);
  const secs = Number(m[1]);
  const frac = m[2] ? m[2].padEnd(9, "0") : "000000000";
  const ns = secs * 1_000_000_000 + Number(frac);
  if (!Number.isSafeInteger(ns)) throw new Error(`duration ${JSON.stringify(s)}: ns exceeds safe integer range`);
  return ns;
}

/** Nanoseconds -> "M:SS.mmm" (millisecond display precision), reusing formatLapTime. Assumes ns >= 0 (callers pass parseDuration output or non-negative derived ns). */
export function nsToClock(ns: number): string {
  return formatLapTime(Math.round(ns / 1_000_000));
}

/** Nanoseconds delta -> "+0.910s" / "−0.910s" (Unicode minus, 3dp + trailing s). */
export function nsToDeltaSeconds(ns: number): string {
  const sign = ns >= 0 ? "+" : "−";
  return `${sign}${(Math.abs(ns) / 1_000_000_000).toFixed(3)}s`;
}
