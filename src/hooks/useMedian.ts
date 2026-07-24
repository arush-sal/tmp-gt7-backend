import { useEffect, useState } from "react";
import { fetchMedian } from "../lib/sessionApi";
import type { MedianDTO } from "../contract/session";

// The session's per-channel median line. Session-level: fetched on session change and refreshed as
// laps complete (`refreshKey`) so the median pool grows. null until it resolves.
export function useMedian(sessionId: string | null, refreshKey = 0): MedianDTO | null {
  const [data, setData] = useState<MedianDTO | null>(null);
  useEffect(() => {
    if (sessionId == null) { setData(null); return; }
    const ctrl = new AbortController();
    let active = true;
    fetchMedian(sessionId, undefined, undefined, ctrl.signal)
      .then(d => { if (active) setData(d); })
      .catch(e => { if (e.name !== "AbortError" && active) setData(null); });
    return () => { active = false; ctrl.abort(); };
  }, [sessionId, refreshKey]);
  return data;
}
