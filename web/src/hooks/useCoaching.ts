import { useEffect, useState } from "react";
import { fetchCoaching } from "../lib/sessionApi";
import { nsToClock } from "../lib/format";

// Post-race predicted-best lap from GET /v1/sessions/{id}/coaching. Only fetched when `enabled`
// (i.e. the race is over — the endpoint 409s on an active session; during the race the predicted
// lap comes live from /v1/live enrichment.analysis instead). Returns the formatted lap string, or
// null when there's no prediction / the fetch fails — so the caller can fall back to the live value.
export function useCoaching(sessionId: string | null, enabled: boolean): { predicted: string | null } {
  const [predicted, setPredicted] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled || !sessionId) { setPredicted(null); return; }
    const ctrl = new AbortController();
    let active = true;
    fetchCoaching(sessionId, ctrl.signal)
      .then(rep => {
        if (!active) return;
        const ns = rep.analysis?.predicted_best_ns;
        setPredicted(typeof ns === "number" && ns > 0 ? nsToClock(ns) : null);
      })
      .catch(e => { if (e.name === "AbortError") return; if (active) setPredicted(null); });
    return () => { active = false; ctrl.abort(); };
  }, [sessionId, enabled]);

  return { predicted };
}
