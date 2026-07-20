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

/**
 * Milliseconds -> sector-split display: "SS.mmm" under a minute (the common case for a
 * sector split), "M:SS.mmm" (reusing formatLapTime) at or beyond a minute. null -> PLACEHOLDER.
 */
export function msToSectorTime(ms: number | null): string {
  if (ms === null) return PLACEHOLDER;
  if (ms < 60_000) return (ms / 1_000).toFixed(3);
  return formatLapTime(ms);
}

/**
 * Per-sector delta in milliseconds -> signed seconds, e.g. "+0.412" / "-0.311".
 * Sign is always shown so the caller can colour-code (+ = slower/red, - = faster/blue).
 */
export function msToSectorDelta(ms: number): string {
  const sign = ms >= 0 ? "+" : "-";
  return `${sign}${(Math.abs(ms) / 1_000).toFixed(3)}`;
}
