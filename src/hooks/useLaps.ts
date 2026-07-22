import { useEffect, useState } from "react";
import { fetchLaps } from "../lib/sessionApi";
import type { LapDTO } from "../contract/session";

type LapsState = { laps: LapDTO[]; loading: boolean; error: boolean };

// Fetches the session's lap metadata list (lap times + sector splits + reliability) once per
// session. Cheap + authoritative — the dropdown/auto-follow stay live-store-driven; this drives the
// sector card + the delta figures for a selected lap. Refetches when the session UUID changes.
export function useLaps(sessionId: string | null): LapsState {
  const [state, setState] = useState<LapsState>({ laps: [], loading: false, error: false });

  useEffect(() => {
    if (!sessionId) { setState({ laps: [], loading: false, error: false }); return; }
    const ctrl = new AbortController();
    let active = true;
    setState({ laps: [], loading: true, error: false });
    fetchLaps(sessionId, ctrl.signal)
      .then(laps => { if (active) setState({ laps, loading: false, error: false }); })
      .catch(e => { if (e.name === "AbortError") return; if (active) setState({ laps: [], loading: false, error: true }); });
    return () => { active = false; ctrl.abort(); };
  }, [sessionId]);

  return state;
}
