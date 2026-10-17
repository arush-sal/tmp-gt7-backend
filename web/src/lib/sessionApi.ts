// src/lib/sessionApi.ts
// Typed fetch wrappers for the backend session/lap endpoints (see contract/session.ts). Relative
// URLs — Vite proxies /v1 -> the backend. Each throws on a non-2xx response; callers surface it.
import type { SessionSummaryDTO, LapDTO, TraceSampleDTO, ReferenceDTO, MedianDTO, PeaksDTO, CoachingReport } from "../contract/session";

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

// The predicted (ideal) lap. ?decimate caps the samples array (default 1500), mirroring /trace.
export const fetchReference = (
  sessionId: string, decimate = 1500, signal?: AbortSignal,
): Promise<ReferenceDTO> =>
  getJSON(`/v1/sessions/${encodeURIComponent(sessionId)}/reference?decimate=${decimate}`, signal);

// The per-channel median line. ?channels filters (absent -> all); ?points sets grid resolution.
export const fetchMedian = (
  sessionId: string, channels?: string[], points?: number, signal?: AbortSignal,
): Promise<MedianDTO> => {
  const q = new URLSearchParams();
  if (channels?.length) q.set("channels", channels.join(","));
  if (points) q.set("points", String(points));
  const qs = q.toString();
  return getJSON(`/v1/sessions/${encodeURIComponent(sessionId)}/median${qs ? `?${qs}` : ""}`, signal);
};

// Speed peaks & valleys for a lap. reference: undefined | "predicted" -> reference A (predicted-best);
// a lap number -> reference B (that recorded lap).
export const fetchPeaks = (
  sessionId: string, lap: number, reference?: number | "predicted", signal?: AbortSignal,
): Promise<PeaksDTO> => {
  const q = new URLSearchParams();
  if (reference != null && reference !== "predicted") q.set("reference", String(reference));
  const qs = q.toString();
  return getJSON(
    `/v1/sessions/${encodeURIComponent(sessionId)}/laps/${lap}/peaks${qs ? `?${qs}` : ""}`,
    signal,
  );
};

export const fetchSessions = (signal?: AbortSignal): Promise<SessionSummaryDTO[]> =>
  getJSON("/v1/sessions", signal);
