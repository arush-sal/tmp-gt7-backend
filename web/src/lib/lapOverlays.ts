// Median + reference overlay columns for the telemetry graphs. Both backend series live on the
// track-fraction axis; the graph x-axis is the selected lap's lap-time. We resample each onto the
// selected lap's per-sample fraction so the overlay column lines up 1:1 with the actual-lap column
// (uPlot then plots actual/median/reference against the same xs). Tyre temp is intentionally
// excluded (per product: no median/reference for tyre); it is the only multi-series card anyway.
import type { Channel } from "./historyStore";
import type { LapView } from "./lapView";
import { resampleByFraction } from "./lapView";
import { radToDeg } from "./format";
import type { MedianDTO, ReferenceDTO, PredictedSampleDTO } from "../contract/session";

// channel -> the predicted-sample field carrying that channel's value. Only channels a single-series
// card can show and that the reference lap carries; tyre channels are deliberately absent.
const REF_FIELD: Partial<Record<Channel, keyof PredictedSampleDTO>> = {
  speed: "speed", throttle: "throttle", brake: "brake", rpm: "rpm", gear: "gear", boost: "boost", fuel: "fuel",
  steering: "steering",
};

/** Reference values for one channel (steering in degrees), in sample order; null when the reference
 *  does not carry the channel (tyres) or has no samples. */
export function referenceValues(reference: ReferenceDTO | null, channel: Channel): number[] | null {
  const field = REF_FIELD[channel];
  if (!field || !reference?.samples?.length) return null;
  // The wire carries steering in radians; the graph (and the lap column it overlays) is in degrees.
  return reference.samples.map(s => field === "steering" ? radToDeg(s.steering) : Number(s[field]));
}

export type Overlay = { median: number[] | null; reference: number[] | null };

/** Build resampled median/reference columns for one channel against a selected lap. Either side is
 *  null when its data is absent (median only covers speed/throttle/brake/rpm/boost/gear; reference
 *  covers the REF_FIELD channels). */
export function overlayColumns(
  analysisLap: LapView | null, channel: Channel, median: MedianDTO | null, reference: ReferenceDTO | null,
): Overlay {
  if (!analysisLap) return { median: null, reference: null };
  // Dev signal: the optional chaining below silently disables an overlay on a contract regression;
  // surface it in dev so a backend shape drift isn't invisible.
  if (import.meta.env?.DEV) {
    if (median && !median.series) console.warn("overlayColumns: median payload missing `series` — overlay disabled");
    if (reference && !Array.isArray(reference.samples)) console.warn("overlayColumns: reference payload missing `samples` — overlay disabled");
  }
  const targets = analysisLap.fractions();

  let medianCol: number[] | null = null;
  const medianSeries = median?.series?.[channel];
  if (median?.fractions?.length && medianSeries?.length) {
    medianCol = resampleByFraction(median.fractions, medianSeries, targets);
  }

  let refCol: number[] | null = null;
  const values = referenceValues(reference, channel);
  if (values && reference) refCol = resampleByFraction(reference.samples.map(s => s.fraction), values, targets);

  return { median: medianCol, reference: refCol };
}
