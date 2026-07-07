import { useEffect, useRef } from "react";
import { subscribeLive, type LiveSource } from "../lib/liveSource";
import { createHistoryStore, type HistoryStore } from "../lib/historyStore";

// Independent live subscription that feeds a per-frame ring buffer WITHOUT React state
// (no re-render per frame). The backend broadcaster supports multiple subscribers; a future
// pass can fan a single subscription to both useSession and this store.
export function useHistory(source: LiveSource = subscribeLive): HistoryStore {
  const storeRef = useRef<HistoryStore>();
  if (!storeRef.current) storeRef.current = createHistoryStore();
  const store = storeRef.current;
  useEffect(() => {
    const unsub = source("/v1/live/stream", {
      onSnapshot: (s) => store.push(s),
      onError: () => { /* keep buffer; graph shows last-good until reconnect */ },
    });
    return unsub;
  }, [source, store]);
  return store;
}
