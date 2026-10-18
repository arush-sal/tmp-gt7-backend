// Analysis mode couples the Track and Session pickers: a session belongs to its dominant confirmed
// track (SessionSummaryDTO.track_slug), sessions without one go under an "Unknown track" key.
import type { TrackListItem } from "../components/track/TrackPicker";

/** UNKNOWN_TRACK is the Track picker key for sessions with no confirmed track (not a real slug). */
export const UNKNOWN_TRACK = "__unknown__";

type S = { id: string; track_slug: string | null };

/** trackKey is the Track picker key a session files under. */
export const trackKey = (s: S): string => s.track_slug ?? UNKNOWN_TRACK;

/** analysisTracks lists only tracks that have stored sessions, by name, with Unknown last if any. */
export function analysisTracks(sessions: S[], names: Record<string, string>): TrackListItem[] {
  const slugs = [...new Set(sessions.map(s => s.track_slug).filter((t): t is string => !!t))];
  const out = slugs.map(slug => ({ slug, name: names[slug] ?? slug })).sort((a, b) => a.name.localeCompare(b.name));
  if (sessions.some(s => !s.track_slug)) out.push({ slug: UNKNOWN_TRACK, name: "Unknown track" });
  return out;
}

/**
 * reconcileAnalysis returns the consistent (session, track) pair: the selected session if it is on the
 * selected track (or no track is picked yet), else the most recent session on the picked track, else
 * the most recent session overall. `sessions` must be newest first.
 */
export function reconcileAnalysis(sessions: S[], id: string | null, track: string | null): { id: string | null; track: string | null } {
  const cur = sessions.find(s => s.id === id);
  const pick = cur && (track === null || trackKey(cur) === track) ? cur
    : sessions.find(s => trackKey(s) === track) ?? sessions[0];
  return pick ? { id: pick.id, track: trackKey(pick) } : { id: null, track: null };
}

type L = { lap_number: number; duration_ns: number; complete: boolean; valid: boolean };

/**
 * defaultAnalysisLap picks the lap analysis opens on: the fastest valid complete lap, else the last
 * complete lap, else "all". Formation (0) and cooldown (> totalLaps, when totalLaps > 0) laps never count,
 * so a session-end fragment (incomplete, a fraction of a second) is never the default.
 */
export function defaultAnalysisLap(laps: L[], totalLaps: number): number | "all" {
  const racing = laps.filter(l => l.lap_number >= 1 && (totalLaps <= 0 || l.lap_number <= totalLaps) && l.complete && l.duration_ns > 0);
  const best = racing.filter(l => l.valid).sort((a, b) => a.duration_ns - b.duration_ns)[0];
  const last = racing.sort((a, b) => b.lap_number - a.lap_number)[0];
  return best?.lap_number ?? last?.lap_number ?? "all";
}
