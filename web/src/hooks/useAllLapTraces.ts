import { useEffect, useState } from "react";
import { fetchLapTrace } from "../lib/sessionApi";
import { getLap, putLap } from "../lib/lapCache";
import { lapViewFromSamples, traceToSamples, type LapView } from "../lib/lapView";

// Resolves EVERY given completed lap's trace as LapViews for the "All laps" overlay — cache-first
// (in-session laps served with no fetch), else the decimated backend trace, warming the cache. One
// combined fetch per lap; empty `laps` (not in "all" mode) => no work. Keyed by (sessionId, laps).
export function useAllLapTraces(sessionId: string | null, laps: number[]): LapView[] {
  const [views, setViews] = useState<LapView[]>([]);
  const key = laps.join(",");
  useEffect(() => {
    if (sessionId == null || laps.length === 0) { setViews([]); return; }
    const ctrl = new AbortController();
    let active = true;
    Promise.all(laps.map(async n => {
      const hit = getLap(sessionId, n);
      if (hit) return lapViewFromSamples(hit);
      const samples = traceToSamples(await fetchLapTrace(sessionId, n, 1500, ctrl.signal));
      putLap(sessionId, n, samples);
      return lapViewFromSamples(samples);
    }))
      .then(vs => { if (active) setViews(vs); })
      .catch(e => { if (e.name !== "AbortError" && active) setViews([]); });
    return () => { active = false; ctrl.abort(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, key]);
  return views;
}
