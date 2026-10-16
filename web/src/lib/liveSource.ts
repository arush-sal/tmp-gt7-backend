import type { LiveSnapshot } from "../contract/live";
import { validateLive } from "./validateLive";

export type LiveHandlers = {
  onSnapshot: (s: LiveSnapshot) => void;
  onError: (e: Error) => void;
};

/** A subscription factory: opens a source, pushes validated snapshots, returns an unsubscribe fn. */
export type LiveSource = (url: string, handlers: LiveHandlers) => () => void;

export const DEFAULT_STREAM_URL = "/v1/live/stream";

/** Default LiveSource: a browser EventSource over the SSE stream. Events are unnamed (onmessage). */
export const subscribeLive: LiveSource = (url, { onSnapshot, onError }) => {
  const es = new EventSource(url);
  es.onmessage = (ev: MessageEvent) => {
    try {
      onSnapshot(validateLive(JSON.parse(ev.data)));
    } catch (err) {
      onError(err instanceof Error ? err : new Error(String(err)));
    }
  };
  es.onerror = () => onError(new Error("live stream connection error"));
  return () => es.close();
};
