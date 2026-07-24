import { useEffect, useState } from "react";
import { fetchPeaks } from "../lib/sessionApi";
import type { PeaksDTO } from "../contract/session";

type PeaksState = { data: PeaksDTO | null; loading: boolean; error: boolean };

// Speed peaks & valleys for one completed lap, vs a reference (undefined/"predicted" -> ideal lap;
// a lap number -> that recorded lap). No client cache: the payload is tiny (~10-30 points) and
// recomputed on request. Mirrors useLapTrace's abort-on-change effect.
export function usePeaks(
  sessionId: string | null, lap: number | null, reference?: number | "predicted",
): PeaksState {
  const [state, setState] = useState<PeaksState>({ data: null, loading: false, error: false });
  useEffect(() => {
    if (sessionId == null || lap == null) { setState({ data: null, loading: false, error: false }); return; }
    const ctrl = new AbortController();
    let active = true;
    setState({ data: null, loading: true, error: false });
    fetchPeaks(sessionId, lap, reference, ctrl.signal)
      .then(d => { if (active) setState({ data: d, loading: false, error: false }); })
      .catch(e => { if (e.name === "AbortError") return; if (active) setState({ data: null, loading: false, error: true }); });
    return () => { active = false; ctrl.abort(); };
  }, [sessionId, lap, reference]);
  return state;
}
