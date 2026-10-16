// ---------------------------------------------------------------------------
// Corner geometry + per-corner analysis shapes.
//
// These describe the corner/coaching data the UI is built to render. The
// backend does not emit them yet (coaching is a `status:"pending"` passthrough),
// so nothing populates these at runtime today — the corner/coaching panels show
// their pending/empty states. When the backend ships corner-level analysis,
// wire the real values into these shapes and the panels light up unchanged.
// ---------------------------------------------------------------------------

/** A corner as it appears on the track map (SVG coords + delta summary). */
export interface Corner {
  id: number;
  name: string;
  sector: number;
  /** seconds lost (+) or gained (-) vs reference */
  delta: number;
  /** SVG x coordinate */
  cx: number;
  /** SVG y coordinate */
  cy: number;
}

/** Full per-corner breakdown shown in the Corner Detail view. */
export interface CornerAnalysis extends Corner {
  brakeDelta: number;         // seconds
  entrySpeedDelta: number;    // km/h - positive = driver entered faster than ref
  apexSpeedDelta: number;     // km/h - negative = driver was slower at apex
  exitSpeedDelta: number;     // km/h
  refBrakePoint: number;      // metres from corner reference point
  actualBrakePoint: number;
  refApexSpeed: number;       // km/h
  actualApexSpeed: number;
  throttlePickupDelta: number; // seconds late vs reference
  coaching: {
    problem: string;
    action: string;
    evidence: string;
    expectedGain: number;
    confidence: "High" | "Medium" | "Low";
  };
}
