import { useEffect, useRef } from "react";
import { subscribeLive, type LiveSource } from "../lib/liveSource";
import { createHistoryStore, type HistoryStore } from "../lib/historyStore";
import { putLap } from "../lib/lapCache";

// Independent live subscription that feeds a per-frame ring buffer WITHOUT React state
// (no re-render per frame). The backend broadcaster supports multiple subscribers; a future
// pass can fan a single subscription to both useSession and this store.
//
// On each lap rollover the store hands the finished lap's samples to lapCache.putLap, keyed by
// the current sessionId — so a completed-lap selection serves instantly from the live data. The
// buffer itself is no longer persisted to localStorage (completed laps are backend-served / cached).
export function useHistory(source: LiveSource = subscribeLive, sessionId: string | null = null): HistoryStore {
  // Read the latest sessionId from a ref so the onLapComplete closure (created once) always caches
  // against the current session, not the one at store-construction time.
  const sessionIdRef = useRef(sessionId);
  sessionIdRef.current = sessionId;

  const storeRef = useRef<HistoryStore>();
  if (!storeRef.current) {
    storeRef.current = createHistoryStore((lap, samples) => {
      const sid = sessionIdRef.current;
      if (sid) putLap(sid, lap, samples);
    });
  }
  const store = storeRef.current;

  useEffect(() => {
    const unsub = source("/v1/live/stream", {
      onSnapshot: (s) => store.push(s),
      onError: () => { /* keep buffer; graph shows last-good until reconnect */ },
    });
    return () => { unsub(); };
  }, [source, store]);

  return store;
}
