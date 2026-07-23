import { useEffect, useState } from "react";
import { fetchLaps } from "../lib/sessionApi";
import type { LapDTO } from "../contract/session";

type LapsState = { laps: LapDTO[]; loading: boolean; error: boolean };

// Fetches the session's lap metadata list (lap times + sector splits + reliability). This is the
// AUTHORITATIVE lap list — it drives the dropdown + auto-follow (so a page refresh restores every
// completed lap from the backend, not the live in-memory counter which resets on reload) as well as
// the sector card. Refetches when the session UUID changes OR `revalidate` changes — pass the live
// completed-lap count so a freshly-finished lap is picked up mid-race.
export function useLaps(sessionId: string | null, revalidate = 0): LapsState {
  const [state, setState] = useState<LapsState>({ laps: [], loading: false, error: false });

  useEffect(() => {
    if (!sessionId) { setState({ laps: [], loading: false, error: false }); return; }
    const ctrl = new AbortController();
    let active = true;
    setState(prev => ({ laps: prev.laps, loading: true, error: false })); // keep prior laps while revalidating
    fetchLaps(sessionId, ctrl.signal)
      .then(laps => { if (active) setState({ laps, loading: false, error: false }); })
      .catch(e => { if (e.name === "AbortError") return; if (active) setState(prev => ({ laps: prev.laps, loading: false, error: true })); });
    return () => { active = false; ctrl.abort(); };
  }, [sessionId, revalidate]);

  return state;
}
