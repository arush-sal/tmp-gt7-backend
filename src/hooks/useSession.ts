// src/hooks/useSession.ts
import { useEffect, useState } from "react";
import type { LiveSnapshot } from "../contract/live";
import { subscribeLive, DEFAULT_STREAM_URL, type LiveSource } from "../lib/liveSource";

export type SessionState = {
  snapshot: LiveSnapshot | null;
  error: Error | null;
};

/**
 * Subscribes to the live stream and re-renders on each pushed snapshot.
 * `source` is injectable for tests (default: real EventSource via subscribeLive).
 * On a stream error the last-good snapshot is retained and `error` is surfaced.
 * `source` must be a stable reference (module-level or memoized); a new identity each render resubscribes every render.
 */
export function useSession(
  url: string = DEFAULT_STREAM_URL,
  source: LiveSource = subscribeLive,
): SessionState {
  const [snapshot, setSnapshot] = useState<LiveSnapshot | null>(null);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    let active = true; // guards against a late callback from a torn-down subscription
    const unsubscribe = source(url, {
      onSnapshot: (s) => { if (active) { setSnapshot(s); setError(null); } },
      onError: (e) => { if (active) setError(e); },
    });
    return () => { active = false; unsubscribe(); };
  }, [url, source]);

  return { snapshot, error };
}
