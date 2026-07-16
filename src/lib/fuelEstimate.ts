/**
 * fuelEstimate.ts — laps-left projection from the last completed lap's fuel burn.
 *
 * `usedPerLapPct` comes from a two-sample delta (fuel_pct at lap start vs lap end) and
 * can be tiny/noisy (near-zero, or even slightly negative from a refuel/rounding blip).
 * Dividing the current tank by a near-zero burn rate produces an implausible laps-left
 * (e.g. 200+), so we only compute a projection when the burn rate is a sane positive
 * number above a small floor, and drop results that are still implausibly large.
 */

const MIN_USED_PER_LAP_PCT = 0.5;
const MAX_PLAUSIBLE_LAPS_LEFT = 200;

/**
 * Projects laps remaining from `currentFuelPct` / `usedPerLapPct`. Returns null (caller
 * shows "-") whenever either input is missing, the burn rate is at/below the floor, or
 * the projected value isn't a sane finite non-negative number under the plausibility cap.
 */
export function estimateLapsLeft(
  currentFuelPct: number | null,
  usedPerLapPct: number | null,
): number | null {
  if (currentFuelPct === null || usedPerLapPct === null) return null;
  if (!(usedPerLapPct >= MIN_USED_PER_LAP_PCT)) return null;

  const lapsLeft = currentFuelPct / usedPerLapPct;
  if (!Number.isFinite(lapsLeft) || lapsLeft < 0 || lapsLeft > MAX_PLAUSIBLE_LAPS_LEFT) return null;
  return lapsLeft;
}
