import { useEffect, useState } from "react";

/** AppMode is how rtps was started (GET /v1/mode): a live PS5 feed, a capture replay, or stored-data analysis only. */
export type AppMode = "live" | "replay" | "analysis";

/** parseMode validates a /v1/mode body; anything unexpected is "live" (today's behaviour is the fallback). */
export function parseMode(body: unknown): AppMode {
  const m = (body as { mode?: unknown } | null)?.mode;
  return m === "replay" || m === "analysis" ? m : "live";
}

/** useMode fetches the server mode once: null while loading, then the mode ("live" on any failure). */
export function useMode(): AppMode | null {
  const [mode, setMode] = useState<AppMode | null>(null);
  useEffect(() => {
    let active = true;
    fetch("/v1/mode")
      .then(r => (r.ok ? r.json() : null))
      .then(b => parseMode(b), () => "live" as const)
      .then(m => { if (active) setMode(m); });
    return () => { active = false; };
  }, []);
  return mode;
}
