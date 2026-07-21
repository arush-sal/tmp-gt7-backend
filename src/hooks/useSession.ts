// src/hooks/useSession.ts
import { useEffect, useRef, useState } from "react";
import type { LiveSnapshot } from "../contract/live";
import { subscribeLive, DEFAULT_STREAM_URL, type LiveSource } from "../lib/liveSource";
import { deriveLive, stepRace, initialRaceKey, type RaceKeyState } from "../lib/raceState";
import { mergeSticky, type StickyView } from "../lib/stickyView";
import { toLiveView } from "../lib/liveView";
import { loadVersioned, saveVersioned } from "../lib/persistState";

const RACE_STORAGE_KEY = "gt7:race";

type RaceMeta = { raceKey: string | null; lastLap: number; sticky: StickyView | null };

export type SessionState = {
  snapshot: LiveSnapshot | null;
  error: Error | null;
  view: StickyView | null; // sticky-merged display view (null before first frame)
  live: boolean; // deriveLive of the latest snapshot
  raceKey: string | null; // current race identity
  newRaceSeq: number; // increments ONLY on a genuine new-race (stepRace.newRace)
};

/**
 * Subscribes to the live stream and re-renders on each pushed snapshot.
 * `source` is injectable for tests (default: real EventSource via subscribeLive).
 * On a stream error the last-good snapshot is retained and `error` is surfaced.
 * `source` must be a stable reference (module-level or memoized); a new identity each render resubscribes every render.
 *
 * Also derives race lifecycle (deriveLive/stepRace) + a sticky-merged display view, and persists
 * race identity + sticky view to localStorage so a refresh mid-race seeds `raceKey`/`view` before
 * the first frame arrives.
 */
export function useSession(
  url: string = DEFAULT_STREAM_URL,
  source: LiveSource = subscribeLive,
): SessionState {
  const raceRef = useRef<RaceKeyState>(initialRaceKey);
  const stickyRef = useRef<StickyView | null>(null);
  const seqRef = useRef(0);
  const initRef = useRef(false);
  if (!initRef.current) {
    initRef.current = true;
    const saved = loadVersioned<RaceMeta>(RACE_STORAGE_KEY);
    if (saved) {
      raceRef.current = { raceKey: saved.raceKey, lastLap: saved.lastLap ?? 0 };
      stickyRef.current = saved.sticky;
    }
  }

  const [st, setSt] = useState<Omit<SessionState, "error">>(() => ({
    snapshot: null,
    view: stickyRef.current,
    live: false,
    raceKey: raceRef.current.raceKey,
    newRaceSeq: 0,
  }));
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    let active = true; // guards against a late callback from a torn-down subscription
    const unsubscribe = source(url, {
      onSnapshot: (s) => {
        if (!active) return;
        const live = deriveLive(s);
        const { state: rk, newRace } = stepRace(raceRef.current, s);
        raceRef.current = rk;
        if (newRace) { stickyRef.current = null; seqRef.current += 1; } // new race -> fresh sticky
        const view = mergeSticky(stickyRef.current, toLiveView(s), live);
        stickyRef.current = view;
        setSt({ snapshot: s, view, live, raceKey: rk.raceKey, newRaceSeq: seqRef.current });
        setError(null);
      },
      onError: (e) => { if (active) setError(e); },
    });

    const flush = () => saveVersioned(RACE_STORAGE_KEY, { raceKey: raceRef.current.raceKey, lastLap: raceRef.current.lastLap, sticky: stickyRef.current });
    const id = setInterval(flush, 2000);
    window.addEventListener("beforeunload", flush);
    window.addEventListener("pagehide", flush);

    return () => {
      active = false;
      unsubscribe();
      clearInterval(id);
      window.removeEventListener("beforeunload", flush);
      window.removeEventListener("pagehide", flush);
    };
  }, [url, source]);

  return { ...st, error };
}
