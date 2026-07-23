// src/lib/sessionApi.ts
// Typed fetch wrappers for the backend session/lap endpoints (see contract/session.ts). Relative
// URLs — Vite proxies /v1 -> the backend. Each throws on a non-2xx response; callers surface it.
import type { LapDTO, TraceSampleDTO, ReferenceDTO, CoachingReport } from "../contract/session";

async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, signal ? { signal } : undefined);
  if (!res.ok) throw new Error(`${url} -> ${res.status}`);
  return res.json() as Promise<T>;
}

export const fetchLaps = (sessionId: string, signal?: AbortSignal): Promise<LapDTO[]> =>
  getJSON(`/v1/sessions/${encodeURIComponent(sessionId)}/laps`, signal);

export const fetchLapTrace = (
  sessionId: string, lap: number, decimate: number, signal?: AbortSignal,
): Promise<TraceSampleDTO[]> =>
  getJSON(`/v1/sessions/${encodeURIComponent(sessionId)}/laps/${lap}/trace?decimate=${decimate}`, signal);

export const fetchCoaching = (sessionId: string, signal?: AbortSignal): Promise<CoachingReport> =>
  getJSON(`/v1/sessions/${encodeURIComponent(sessionId)}/coaching`, signal);

export const fetchReference = (
  sessionId: string, channels?: string[], points?: number, signal?: AbortSignal,
): Promise<ReferenceDTO> => {
  const q = new URLSearchParams();
  if (channels?.length) q.set("channels", channels.join(","));
  if (points) q.set("points", String(points));
  const qs = q.toString();
  return getJSON(`/v1/sessions/${encodeURIComponent(sessionId)}/reference${qs ? `?${qs}` : ""}`, signal);
};
