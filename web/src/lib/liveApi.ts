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
