import { useEffect, useState } from "react";
import { fetchReference } from "../lib/sessionApi";
import type { ReferenceDTO } from "../contract/session";

// The session's predicted (ideal) reference lap. Session-level: fetched on session change and
// refreshed as laps complete (`refreshKey`, e.g. completed-lap count) so it improves with more
// data. null until it resolves. Mirrors useLapTrace's abort-on-change effect shape.
export function useReference(sessionId: string | null, refreshKey = 0): ReferenceDTO | null {
  const [data, setData] = useState<ReferenceDTO | null>(null);
  useEffect(() => {
    if (sessionId == null) { setData(null); return; }
    const ctrl = new AbortController();
    let active = true;
    fetchReference(sessionId, undefined, ctrl.signal)
      .then(d => { if (active) setData(d); })
      .catch(e => { if (e.name !== "AbortError" && active) setData(null); });
    return () => { active = false; ctrl.abort(); };
  }, [sessionId, refreshKey]);
  return data;
}
