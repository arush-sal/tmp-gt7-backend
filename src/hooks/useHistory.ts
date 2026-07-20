import { useEffect, useRef } from "react";
import { subscribeLive, type LiveSource } from "../lib/liveSource";
import { createHistoryStore, type HistoryStore, type PersistedHistory } from "../lib/historyStore";
import { loadVersioned, saveVersioned } from "../lib/persistState";

const STORAGE_KEY = "gt7:history";

function loadPersisted(store: HistoryStore) {
  const blob = loadVersioned<PersistedHistory>(STORAGE_KEY);
  if (blob) store.load(blob);
}

function persist(store: HistoryStore) {
  const d = store.dump();
  if (d) saveVersioned(STORAGE_KEY, d);
}

// Independent live subscription that feeds a per-frame ring buffer WITHOUT React state
// (no re-render per frame). The backend broadcaster supports multiple subscribers; a future
// pass can fan a single subscription to both useSession and this store.
//
// The buffer is persisted to localStorage so a page refresh restores the accumulated trace,
// graphs, sector splits and fuel history rather than starting blank. It rehydrates
// synchronously on first render (before subscribing, so no race with incoming frames) and
// flushes periodically + on page hide/unload. Deliberately NOT flushed on unmount: that keeps
// jsdom tests isolated (no unload events fire) while real refreshes still persist via the
// pagehide/beforeunload handlers.
export function useHistory(source: LiveSource = subscribeLive): HistoryStore {
  const storeRef = useRef<HistoryStore>();
  if (!storeRef.current) {
    storeRef.current = createHistoryStore();
    loadPersisted(storeRef.current);
  }
  const store = storeRef.current;

  useEffect(() => {
    const unsub = source("/v1/live/stream", {
      onSnapshot: (s) => store.push(s),
      onError: () => { /* keep buffer; graph shows last-good until reconnect */ },
    });
    const flush = () => persist(store);
    const id = setInterval(flush, 2000);
    window.addEventListener("beforeunload", flush);
    window.addEventListener("pagehide", flush);
    return () => {
      unsub();
      clearInterval(id);
      window.removeEventListener("beforeunload", flush);
      window.removeEventListener("pagehide", flush);
    };
  }, [source, store]);

  return store;
}
