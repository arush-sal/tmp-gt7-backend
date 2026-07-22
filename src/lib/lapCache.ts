// src/lib/lapCache.ts
// In-memory, session-scoped cache of completed laps' full-fidelity samples, populated live as each
// lap finishes (historyStore.onLapComplete -> putLap). Keyed by (sessionId, lap_number). Lets a lap
// selection serve instantly from the live data instead of round-tripping the backend; a cold client
// (post-refresh) misses and falls back to fetching. Not persisted (no localStorage — Bug 4 stays dead).
import type { LapSample } from "./lapView";

const cache = new Map<string, LapSample[]>();
const key = (sessionId: string, lap: number) => `${sessionId}:${lap}`;

export function putLap(sessionId: string, lap: number, samples: LapSample[]): void {
  cache.set(key(sessionId, lap), samples);
}
export function getLap(sessionId: string, lap: number): LapSample[] | undefined {
  return cache.get(key(sessionId, lap));
}
/** Clear all entries (call on a new race — the new session's laps use fresh keys anyway). */
export function resetLapCache(): void {
  cache.clear();
}
