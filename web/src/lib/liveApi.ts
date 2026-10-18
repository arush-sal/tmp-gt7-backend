// Typed wrapper for POST /v1/live/recording. sessionId = the id the UI shows (enrichment.session_id
// or ""); the backend applies OFF only if it matches the stream's session. 409 = no session open or
// "session changed" (see .message from the error JSON); 503 = telemetry stalled; 400 = bad body.
export class RecordingError extends Error {
  constructor(public status: number, message = `/v1/live/recording -> ${status}`) { super(message); }
}

export async function setRecording(
  recording: boolean, sessionId: string, signal?: AbortSignal,
): Promise<{ recording: boolean; session_id: string }> {
  const res = await fetch("/v1/live/recording", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ recording, session_id: sessionId }),
    signal,
  });
  if (!res.ok) {
    let msg: string | undefined;
    try { msg = (await res.json())?.error; } catch { /* no JSON body */ }
    throw new RecordingError(res.status, msg);
  }
  return res.json();
}

// POST/DELETE /v1/live/track: the user confirms the track (or hands it back to auto-detect).
// POST 200 {track_slug, applies_to: "current"|"next", session_id}; 404 unknown track; 415/403 = CSRF guard.
export class TrackError extends Error {
  constructor(public status: number, message = `/v1/live/track -> ${status}`) { super(message); }
}

/** trackErrorText maps a failed pick to the chip text. 415/403 = backend security rejection. */
export function trackErrorText(e: unknown): string {
  const s = e instanceof TrackError ? e.status : 0;
  if (s === 404) return "Unknown track";
  if (s === 415 || s === 403) return "Request rejected";
  return "Couldn't confirm track";
}

async function trackCall<T>(method: "POST" | "DELETE", body?: string): Promise<T> {
  const res = await fetch("/v1/live/track", { method, headers: { "Content-Type": "application/json" }, body });
  if (!res.ok) throw new TrackError(res.status);
  return res.json();
}

export const setLiveTrack = (slug: string) =>
  trackCall<{ track_slug: string; applies_to: "current" | "next"; session_id?: string }>("POST", JSON.stringify({ track_slug: slug }));
export const clearLiveTrack = () => trackCall<{ track_slug: null }>("DELETE");
