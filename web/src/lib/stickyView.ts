import type { LiveView, TyreTemp } from "./liveView";
import { PLACEHOLDER } from "./format";

export type StickyView = LiveView & { live: boolean };

type ScalarField = { [K in keyof LiveView]: LiveView[K] extends string ? K : never }[keyof LiveView];

const SCALAR_FIELDS: readonly ScalarField[] = [
  "speed", "gear", "rpm", "lap", "position", "fuelPct", "currentLapTime",
  "delta", "predicted", "bestLap", "lastLap", "steering", "surface", "carCategory",
] as const;

function keepCorner(prev: TyreTemp, next: TyreTemp): TyreTemp {
  return next.value === PLACEHOLDER && prev.value !== PLACEHOLDER ? prev : next;
}

/** Merge `next` over `prev`: carry the last non-placeholder value forward per field. */
export function mergeSticky(prev: StickyView | null, next: LiveView, live: boolean): StickyView {
  if (prev === null) return { ...next, live };

  const result = { ...next, live } as StickyView;
  const resultScalars: Record<ScalarField, string> = result;
  const prevScalars: Record<ScalarField, string> = prev;
  const nextScalars: Record<ScalarField, string> = next;

  for (const f of SCALAR_FIELDS) {
    if (nextScalars[f] === PLACEHOLDER && prevScalars[f] !== PLACEHOLDER) {
      resultScalars[f] = prevScalars[f];
    }
  }

  result.tyreTemps = {
    fl: keepCorner(prev.tyreTemps.fl, next.tyreTemps.fl),
    fr: keepCorner(prev.tyreTemps.fr, next.tyreTemps.fr),
    rl: keepCorner(prev.tyreTemps.rl, next.tyreTemps.rl),
    rr: keepCorner(prev.tyreTemps.rr, next.tyreTemps.rr),
  };

  if (result.delta !== next.delta) result.deltaTone = prev.deltaTone;
  if (result.fuelPct !== next.fuelPct) result.fuelTone = prev.fuelTone;

  return result;
}
