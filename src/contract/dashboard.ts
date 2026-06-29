// ---------------------------------------------------------------------------
// GT7 Race Engineer — Web Dashboard data contract
//
// This is the response shape served by the API / Presentation Layer (REST JSON
// or protobuf). It is NOT the raw GT7 UDP packet (GTData) — the Analysis +
// Coach engines pre-compute everything stateful/historical, and the frontend
// only applies pure presentation transforms (ms -> "1:57.842", standing ->
// colour).
//
// One object = one full snapshot of "right now". It carries everything needed
// to repaint the whole UI from cold (survives a refresh / reconnect).
// ---------------------------------------------------------------------------

// --- Tyres -----------------------------------------------------------------
export type Compound = "RS" | "RM" | "RH" | "IM" | "W"; // soft/med/hard/inter/wet
export type TyrePosition = "FL" | "FR" | "RL" | "RR";

export type Tyre = {
  position: TyrePosition;
  temp: number;   // °C
  wear: number;   // 0–100 (% life remaining); frontend bands it: >=85 green, 60–85 amber, <60 red
  compound: Compound;
};

// --- Timing ranks ----------------------------------------------------------
// gridFastest ⊃ personalBest: a grid-fastest time is necessarily also a PB,
// so we only ever store the single highest rank. Absence (optional field)
// means "ordinary, no highlight" — no "none" literal needed.
// Frontend maps: gridFastest -> purple, personalBest -> blue.
export type Standing = "gridFastest" | "personalBest";

// --- Lap (status strip: Last + Best, plus the live current-lap delta) ------
export type LapTimeRecord = {
  number: number;                  // current lap number
  total: number;                   // total laps in the race
  liveDelta: number;               // current in-progress lap delta vs reference (ms)
  newLap: boolean;                 // true -> frontend (re)starts the live lap timer

  previousLapTime?: number;        // last completed lap (ms); absent before lap 1 completes
  previousLapStanding?: Standing;  // last lap can be ordinary -> optional

  bestLapTime: number;             // session best lap (ms) — the "Best lap 1:57.842" cell
  bestLapStanding: Standing;       // a best lap is always >= personalBest -> required
};

// --- Sectors (3 per lap) ---------------------------------------------------
export type SectorTimeRecord = {
  number: number;                     // 1 | 2 | 3
  liveDelta: number;                  // sector delta vs reference (ms)
  newSector: boolean;                 // true -> frontend (re)starts the sector timer
  finishTime: number;                 // completed sector time (ms)
  previousSectorStanding?: Standing;  // a sector can be ordinary -> optional
};

// --- Fuel ------------------------------------------------------------------
export type Fuel = {
  map: number;        // current fuel map setting
  remaining: number;  // fuel remaining (litres)
  worthLap: number;   // laps' worth of fuel left
};

// --- Race position ---------------------------------------------------------
export type RacePosition = {
  current: number;
  total: number;
};

// --- Coaching card ---------------------------------------------------------
export type Turn = {
  count: number;   // turn number
  name?: string;   // e.g. "Hairpin" (optional — not every corner is named)
};

export type Suggestion = {
  turn: Turn;
  problem: string;
  action: string;
  expectedGain: number; // seconds the action is projected to save
};

// --- Root snapshot ---------------------------------------------------------
export type DashboardSnapshot = {
  position: RacePosition;
  lap: LapTimeRecord;
  sectors: SectorTimeRecord[];
  tyres: Tyre[];
  fuel: Fuel;
  coach: Suggestion;
};
