import { useState } from "react";
import { setRecording, RecordingError } from "../lib/liveApi";

interface Props {
  live: boolean;        // connection state (fresh live telemetry), same signal the old StatusDot showed
  available: boolean;   // a live session exists and we are not viewing a stored one
  sessionId: string;    // enrichment.session_id or "" (sent so the backend can detect a stale view)
  duplicate?: boolean;  // live feed replays a stored session: OFF must not imply the original is deleted
  recording: boolean;   // from enrichment.recording (absent => true); updates on the next snapshot
  send?: typeof setRecording;
}

function errorText(e: unknown): string {
  if (e instanceof RecordingError) {
    if (e.status === 409) return /session changed/i.test(e.message) ? "Session changed, try again" : "No session to record";
    if (e.status === 503) return "Telemetry stalled, recording unchanged";
  }
  return "Couldn't change recording";
}

export default function RecordToggle({ available, recording, sessionId, duplicate = false, live, send = setRecording }: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const clickable = available && live;
  if (!clickable) {
    // Connection state only: OFFLINE (grey), or LIVE while recording controls don't apply (stored session).
    return (
      <span className={`chip ${live ? "chip--green" : "chip--grey"}`} role="status" aria-label={live ? "live" : "not live"}
        title={live ? "Recording controls apply to the live session" : "No live telemetry"}>
        {live ? "● LIVE" : "● OFFLINE"}
      </span>
    );
  }

  const click = async () => {
    const msg = duplicate
      ? "Stop recording this replay? The stored original session is kept."
      : "Stop recording? This discards every lap stored for the current session.";
    if (recording && !window.confirm(msg)) return;
    setBusy(true); setError(null);
    try { await send(!recording, sessionId); }
    catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  };

  return (
    <>
      <button type="button" className={`chip ${recording ? "chip--red" : "chip--green"}`} disabled={busy} onClick={click}
        aria-pressed={recording}
        title={recording ? "Click to stop recording and discard this session" : "Live telemetry, not recording. Click to record"}>
        {recording ? "● REC" : "● LIVE"}
      </button>
      {error && <span className="chip chip--red" role="alert">{error}</span>}
    </>
  );
}
