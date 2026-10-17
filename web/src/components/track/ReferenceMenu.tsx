import { useState } from "react";
import type { CarDTO, StoredReferenceDTO, TrackLapDTO } from "../../contract/session";
import { nsToClock, parseDuration } from "../../lib/format";
import { clearPin, fetchTrackLaps, pinReference, refErrorText } from "../../lib/referenceApi";

const clock = (d: string): string => { try { return nsToClock(parseDuration(d)); } catch { return "—"; } };
const day = (iso: string): string => { const t = new Date(iso); return Number.isNaN(t.getTime()) ? iso : t.toLocaleDateString(); };

/** carTitle: the car details we have, for tooltips. */
export const carTitle = (c: CarDTO): string =>
  `${c.name ?? "Unknown car"} (id ${c.id}${c.category ? `, ${c.category}` : ""})`;

/**
 * ReferenceMenu is the track's stored-reference chip ("Ref 1:30.685 · pinned") with a native
 * <details> menu: "Change…" lists stored laps for the track (choose one to pin), "Clear pin" (pinned only).
 * `onChanged` fires after a successful pin/clear so the caller refetches the reference.
 */
export default function ReferenceMenu({ slug, stored, onChanged }: {
  slug: string; stored: StoredReferenceDTO | null; onChanged: () => void;
}) {
  const [laps, setLaps] = useState<TrackLapDTO[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (!stored) return <span className="chip chip--dim" title="No stored lap for this track">no stored ref</span>;

  const tip = [
    carTitle(stored.car),
    `recorded ${day(stored.recorded_at)}`,
    `session lap ${stored.lap_number}`,
  ].join("\n");

  const run = async (fn: () => Promise<unknown>) => {
    setError(null);
    try { await fn(); setLaps(null); onChanged(); } catch (e) { setError(refErrorText(e)); }
  };
  const openList = async () => {
    setError(null);
    try { setLaps(await fetchTrackLaps(slug)); } catch (e) { setError(refErrorText(e)); }
  };

  return (
    <span className="ref-menu">
      <details>
        <summary className="chip" title={tip}>
          Ref {clock(stored.lap_time)} · {stored.source}
        </summary>
        <div className="ref-menu__pop card">
          <button type="button" className="chip" onClick={openList}>Change…</button>
          {stored.source === "pinned" && (
            <button type="button" className="chip" onClick={() => run(() => clearPin(slug))}>Clear pin</button>
          )}
          {laps && (
            <ul className="ref-menu__list" aria-label="Stored laps">
              {laps.length === 0 && <li className="text-muted">no stored laps</li>}
              {laps.map(l => (
                <li key={`${l.session_id}:${l.lap_number}`}>
                  <button type="button" className="chip" title={carTitle(l.car)}
                    onClick={() => run(() => pinReference(slug, l.session_id, l.lap_number))}>
                    <span className="mono">{clock(l.lap_time)}</span> · {l.car.name ?? `car ${l.car.id}`} · {day(l.recorded_at)}{l.pinned ? " · ★ pinned" : ""}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </details>
      {error && <span className="chip chip--red" role="alert">{error}</span>}
    </span>
  );
}
