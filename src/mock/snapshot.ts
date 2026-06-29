import type { DashboardSnapshot } from "../contract/dashboard";

// ---------------------------------------------------------------------------
// Extended types needed by the UI that aren't (yet) in the backend contract.
//
// NOTE for tutoring: extending the contract to include corner geometry and
// per-corner analysis data is a future task. For now these types live here
// alongside the mock data they describe.
// ---------------------------------------------------------------------------

/** Session-level metadata — not yet in DashboardSnapshot. */
export interface SessionMeta {
  trackName: string;
  trackLayout: string;
  carName: string;
  pp: number;
}

/** A corner as it appears on the track map (SVG coords + delta summary). */
export interface Corner {
  id: number;
  name: string;
  sector: number;
  /** seconds lost (+) or gained (−) vs reference */
  delta: number;
  /** SVG x coordinate */
  cx: number;
  /** SVG y coordinate */
  cy: number;
}

/** Full per-corner breakdown shown in the left-panel Corner Detail view. */
export interface CornerAnalysis extends Corner {
  brakeDelta: number;         // seconds
  entrySpeedDelta: number;    // km/h — positive = driver entered faster than ref
  apexSpeedDelta: number;     // km/h — negative = driver was slower at apex
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

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

export const mockMeta: SessionMeta = {
  trackName: "Suzuka Circuit",
  trackLayout: "Full",
  carName: "Ferrari 296 GT3",
  pp: 620,
};

export const mockSnapshot: DashboardSnapshot = {
  position: { current: 3, total: 16 },
  lap: {
    number: 8,
    total: 20,
    liveDelta: -910,
    newLap: false,
    previousLapTime: 117_660,
    bestLapTime: 117_842,
    bestLapStanding: "personalBest",
  },
  sectors: [
    { number: 1, liveDelta:  122, newSector: false, finishTime: 32_421 },
    { number: 2, liveDelta:  -54, newSector: false, finishTime: 41_008, previousSectorStanding: "personalBest" },
    { number: 3, liveDelta:  211, newSector: false, finishTime: 38_774 },
  ],
  tyres: [
    { position: "FL", temp: 88, wear: 88, compound: "RS" },
    { position: "FR", temp: 96, wear: 59, compound: "RS" },
    { position: "RL", temp: 82, wear: 92, compound: "RS" },
    { position: "RR", temp: 90, wear: 86, compound: "RS" },
  ],
  fuel: { map: 4, remaining: 14.2, worthLap: 7 },
  coach: {
    turn: { count: 3, name: "Hairpin" },
    problem: "Braking too early, rotating too late.",
    action: "Brake 12m later, trail-brake longer, target a later apex.",
    expectedGain: 0.18,
  },
};

/** Derived: predicted lap = best + live delta */
export const predictedLapMs = mockSnapshot.lap.bestLapTime + mockSnapshot.lap.liveDelta;

export const mockCorners: Corner[] = [
  { id: 1, name: "T1 Chicane",    sector: 1, delta:  0.204, cx: 498, cy: 360 },
  { id: 2, name: "T2 Fast Right", sector: 1, delta: -0.032, cx: 466, cy: 118 },
  { id: 3, name: "T3 Hairpin",    sector: 2, delta:  0.321, cx: 283, cy: 211 },
  { id: 4, name: "T4 Fast Left",  sector: 2, delta:  0.156, cx:  90, cy: 266 },
  { id: 5, name: "T5 Exit",       sector: 3, delta: -0.018, cx: 162, cy: 405 },
];

export const mockCornerDetails: CornerAnalysis[] = [
  {
    id: 1, name: "T1 Chicane", sector: 1, delta: 0.204, cx: 498, cy: 360,
    brakeDelta: 0.08, entrySpeedDelta: 5, apexSpeedDelta: -3, exitSpeedDelta: 2,
    refBrakePoint: 98,  actualBrakePoint: 106,
    refApexSpeed: 92,   actualApexSpeed: 89,
    throttlePickupDelta: 0.2,
    coaching: {
      problem:      "Braking slightly early, losing speed through the chicane.",
      action:       "Brake 8m later, keep the car flatter through the exit kerb.",
      evidence:     "Brake point 8m early; apex speed 3 km/h below reference.",
      expectedGain: 0.12,
      confidence:   "Medium",
    },
  },
  {
    id: 2, name: "T2 Fast Right", sector: 1, delta: -0.032, cx: 466, cy: 118,
    brakeDelta: -0.01, entrySpeedDelta: 2, apexSpeedDelta: 1, exitSpeedDelta: 3,
    refBrakePoint: 45,  actualBrakePoint: 44,
    refApexSpeed: 168,  actualApexSpeed: 169,
    throttlePickupDelta: -0.05,
    coaching: {
      problem:      "Slightly conservative on entry — otherwise solid.",
      action:       "Carry 2 km/h more through the apex and trust the exit.",
      evidence:     "Exit speed 3 km/h above reference. Entry can be sharper.",
      expectedGain: 0.03,
      confidence:   "Low",
    },
  },
  {
    id: 3, name: "T3 Hairpin", sector: 2, delta: 0.321, cx: 283, cy: 211,
    brakeDelta: 0.12, entrySpeedDelta: 8, apexSpeedDelta: -6, exitSpeedDelta: 5,
    refBrakePoint: 124, actualBrakePoint: 136,
    refApexSpeed: 84,   actualApexSpeed: 78,
    throttlePickupDelta: 0.4,
    coaching: {
      problem:      "Braking too early and rotating too late.",
      action:       "Brake 12m later, trail-brake longer, target an early apex, delay full throttle until the steering opens.",
      evidence:     "Actual brake point 12m earlier; apex speed 6 km/h lower.",
      expectedGain: 0.18,
      confidence:   "High",
    },
  },
  {
    id: 4, name: "T4 Fast Left", sector: 2, delta: 0.156, cx: 90, cy: 266,
    brakeDelta: 0.05, entrySpeedDelta: 4, apexSpeedDelta: -5, exitSpeedDelta: 2,
    refBrakePoint: 62,  actualBrakePoint: 67,
    refApexSpeed: 143,  actualApexSpeed: 138,
    throttlePickupDelta: 0.25,
    coaching: {
      problem:      "Underdriving the entry — too cautious on brake release.",
      action:       "Release brake 0.3s earlier and carry more speed to the apex.",
      evidence:     "Entry speed 4 km/h high but apex 5 km/h low — braking through the corner.",
      expectedGain: 0.10,
      confidence:   "Medium",
    },
  },
  {
    id: 5, name: "T5 Exit", sector: 3, delta: -0.018, cx: 162, cy: 405,
    brakeDelta: -0.01, entrySpeedDelta: 1, apexSpeedDelta: 0, exitSpeedDelta: 2,
    refBrakePoint: 28,  actualBrakePoint: 27,
    refApexSpeed: 112,  actualApexSpeed: 112,
    throttlePickupDelta: -0.05,
    coaching: {
      problem:      "None — performing at reference level.",
      action:       "Maintain current approach and focus on other corners.",
      evidence:     "All metrics within 2% of reference.",
      expectedGain: 0.02,
      confidence:   "Low",
    },
  },
];
