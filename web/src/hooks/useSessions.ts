import { useCallback, useEffect, useRef, useState } from "react";
import { fetchSessions } from "../lib/sessionApi";
import type { SessionSummaryDTO } from "../contract/session";

type SessionsState = { sessions: SessionSummaryDTO[]; loading: boolean; error: boolean };

// Stored-session list for the picker, newest first. Fetches on mount; `refresh()` refetches (call it
// when the picker opens). Failure sets `error` (kept visible by the picker) and keeps the prior list.
// `liveSessionId` changing (a session started or ended) refetches so the picker is not stale.
export function useSessions(liveSessionId?: string): SessionsState & { refresh: () => void } {
  const [state, setState] = useState<SessionsState>({ sessions: [], loading: false, error: false });
  const ctrl = useRef<AbortController | null>(null);

  const refresh = useCallback(() => {
    ctrl.current?.abort();
    const c = new AbortController();
    ctrl.current = c;
    setState(p => ({ ...p, loading: true, error: false }));
    fetchSessions(c.signal)
      .then(list => {
        const sessions = [...list].sort((a, b) => Date.parse(b.start_time) - Date.parse(a.start_time));
        setState({ sessions, loading: false, error: false });
      })
      .catch(e => { if (e.name === "AbortError") return; setState(p => ({ ...p, loading: false, error: true })); });
  }, []);

  useEffect(() => { refresh(); return () => ctrl.current?.abort(); }, [refresh, liveSessionId]);
  return { ...state, refresh };
}
