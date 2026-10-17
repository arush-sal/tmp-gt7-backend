import { useEffect, useState } from "react";
import { fetchReference } from "../lib/sessionApi";
import type { ReferenceDTO } from "../contract/session";

// The session's predicted (ideal) reference lap. Session-level: fetched on session change and
// refreshed whenever `refreshKey` changes (e.g. completed-lap count, plus the track: its sectors cut
// the lap) so it improves with more data. null until it resolves. Mirrors useLapTrace's abort-on-change effect shape.
export function useReference(sessionId: string | null, refreshKey: number | string = 0): ReferenceDTO | null {
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
