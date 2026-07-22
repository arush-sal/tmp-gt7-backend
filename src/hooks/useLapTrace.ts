import { useEffect, useState } from "react";
import { fetchLapTrace } from "../lib/sessionApi";
import { getLap } from "../lib/lapCache";
import { lapViewFromSamples, lapViewFromTrace, type LapView } from "../lib/lapView";

type TraceState = { view: LapView | null; loading: boolean; error: boolean };

// Resolves a completed lap's trace as a LapView, cache-first: if the live cache holds the lap (the
// common in-session case) it's served instantly at full fidelity with NO fetch; otherwise (cold /
// post-refresh) the decimated backend trace is fetched. Keyed by (sessionId, lap_number) — no
// duration heuristic needed, the backend serves lap_number directly (Phase 2).
export function useLapTrace(sessionId: string | null, lap: number | null, decimate = 1500): TraceState {
  const cached = sessionId != null && lap != null ? getLap(sessionId, lap) : undefined;
  const [state, setState] = useState<TraceState>(
    cached ? { view: lapViewFromSamples(cached), loading: false, error: false } : { view: null, loading: false, error: false });

  useEffect(() => {
    if (sessionId == null || lap == null) { setState({ view: null, loading: false, error: false }); return; }
    const hit = getLap(sessionId, lap);
    if (hit) { setState({ view: lapViewFromSamples(hit), loading: false, error: false }); return; }
    const ctrl = new AbortController();
    let active = true;
    setState({ view: null, loading: true, error: false });
    fetchLapTrace(sessionId, lap, decimate, ctrl.signal)
      .then(dtos => { if (active) setState({ view: lapViewFromTrace(dtos), loading: false, error: false }); })
      .catch(e => { if (e.name === "AbortError") return; if (active) setState({ view: null, loading: false, error: true }); });
    return () => { active = false; ctrl.abort(); };
  }, [sessionId, lap, decimate]);

  return state;
}
