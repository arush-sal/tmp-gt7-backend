// Stored (per-track) reference lap API. Relative URLs; Vite proxies /v1.
import type { StoredReferenceDTO, TrackLapDTO } from "../contract/session";

/** RefError carries the HTTP status so the UI can pick an inline message. */
export class RefError extends Error {
  constructor(public status: number, message = `reference-lap -> ${status}`) { super(message); }
}

/** refErrorText maps a failed pin/clear to the chip text. 415/403 = backend security rejection. */
export function refErrorText(e: unknown): string {
  const s = e instanceof RefError ? e.status : 0;
  if (s === 409) return "Lap not usable as reference";
  if (s === 404) return "Lap not found";
  if (s === 415 || s === 403) return "Request rejected";
  return "Couldn't change reference";
}

const base = (slug: string) => `/v1/tracks/${encodeURIComponent(slug)}/reference-lap`;

// A body is only a reference if it has a samples array (guards against a stub/odd 200).
const isRef = (d: unknown): d is StoredReferenceDTO =>
  !!d && typeof d === "object" && Array.isArray((d as StoredReferenceDTO).samples);

async function fail(res: Response): Promise<never> {
  let msg: string | undefined;
  try { msg = (await res.json())?.error; } catch { /* no JSON body */ }
  throw new RefError(res.status, msg);
}

/** fetchStoredReference: the track's reference, or null on 404 (none stored). */
export async function fetchStoredReference(slug: string, decimate = 1500, signal?: AbortSignal): Promise<StoredReferenceDTO | null> {
  const res = await fetch(`${base(slug)}?decimate=${decimate}`, { signal });
  if (res.status === 404) return null;
  if (!res.ok) return fail(res);
  const d = await res.json();
  return isRef(d) ? d : null;
}

/** pinReference pins a stored lap as the track's reference; throws RefError on 400/404/409/415/403. */
export async function pinReference(slug: string, sessionId: string, lapNumber: number): Promise<StoredReferenceDTO> {
  const res = await fetch(base(slug), {
    method: "PUT",
    headers: { "Content-Type": "application/json" }, // backend rejects non-JSON with 415
    body: JSON.stringify({ session_id: sessionId, lap_number: lapNumber }),
  });
  if (!res.ok) return fail(res);
  return res.json();
}

/** clearPin removes the pin; returns the fallback (fastest) reference or null when nothing remains (404). */
export async function clearPin(slug: string): Promise<StoredReferenceDTO | null> {
  const res = await fetch(base(slug), { method: "DELETE" });
  if (res.status === 404) return null;
  if (!res.ok) return fail(res);
  const d = await res.json();
  return isRef(d) ? d : null;
}

/** fetchTrackLaps lists stored laps for the track, fastest first. */
export async function fetchTrackLaps(slug: string, limit = 20, signal?: AbortSignal): Promise<TrackLapDTO[]> {
  const res = await fetch(`/v1/tracks/${encodeURIComponent(slug)}/laps?limit=${limit}`, { signal });
  if (!res.ok) return fail(res);
  const d = await res.json();
  return Array.isArray(d) ? d : [];
}
