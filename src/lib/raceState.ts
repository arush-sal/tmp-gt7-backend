// src/lib/raceState.ts
// Pure helpers deciding (a) whether the live feed is currently on, and (b) whether a new
// race stream has started. No phase machine, no timers.
import type { LiveSnapshot } from "../contract/live";

// live = we are actively receiving race frames. false when idle, or once the race is over.
export function deriveLive(s: LiveSnapshot | null): boolean {
  if (s === null || s.status !== "live" || s.frame === null) return false;
  const { total_laps, current_lap } = s.frame;
  if (total_laps > 0 && current_lap > total_laps) return false; // race over (GT7 cooldown "11/10")
  return true;
}

export type RaceKeyState = { raceKey: string | null; lastLap: number };
export const initialRaceKey: RaceKeyState = { raceKey: null, lastLap: 0 };

// Advance race identity by one snapshot. newRace=true means THIS snapshot begins a new race and
// the caller must reset its stores. session_id is authoritative; lap-reset is fallback only.
export function stepRace(prev: RaceKeyState, s: LiveSnapshot): { state: RaceKeyState; newRace: boolean } {
  const id = s.enrichment.session_id ?? null;
  const lap = s.frame?.current_lap ?? prev.lastLap;

  const newRace =
    (id != null && prev.raceKey != null && id !== prev.raceKey) ||
    (s.frame?.current_lap === 1 && prev.lastLap > 1);

  const state: RaceKeyState = { raceKey: id ?? prev.raceKey, lastLap: lap };
  return { state, newRace };
}
