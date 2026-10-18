import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { CarDTO, StoredReferenceDTO, TrackLapDTO } from "../../contract/session";
import { nsToClock, parseDuration } from "../../lib/format";
import { clearPin, fetchTrackLaps, pinReference, refErrorText } from "../../lib/referenceApi";

const clock = (d: string): string => { try { return nsToClock(parseDuration(d)); } catch { return "—"; } };
const day = (iso: string): string => { const t = new Date(iso); return Number.isNaN(t.getTime()) ? iso : t.toLocaleDateString(); };

/** carTitle: the car details we have, for tooltips. */
export const carTitle = (c: CarDTO): string =>
  `${c.name ?? "Unknown car"} (id ${c.id}${c.category ? `, ${c.category}` : ""})`;

const POP_MIN_W = 260; // keep in sync with .ref-menu__pop min-width
const HINT = "Stored reference lap for this track. Click to change or clear the pin.";

/**
 * ReferenceMenu is the track's stored-reference chip, always rendered in the same root span so it never
 * disappears. States: confirmed + stored ("Ref 1:30.685 · pinned" + menu); confirmed, nothing stored
 * ("No stored ref" + menu, Change… lists stored laps); `slug` null = track not confirmed, so a dim
 * non-interactive "waiting" chip (`waitingTip` says why) - never pin against a guessed track.
 * `onChanged` fires after a successful pin/clear so the caller refetches the reference.
 */
export default function ReferenceMenu({ slug, stored, onChanged, waitingTip }: {
  slug: string | null; stored: StoredReferenceDTO | null; onChanged: () => void; waitingTip?: string;
}) {
  const [laps, setLaps] = useState<TrackLapDTO[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ top: number; left?: number; right?: number } | null>(null);
  const chipRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);

  // The header-bar clips overflow, so the panel is portalled to <body> and placed (position: fixed)
  // from the chip's rect: below it, left-aligned, or right-aligned when it would run off the viewport.
  useLayoutEffect(() => {
    if (!open || !chipRef.current) return;
    const r = chipRef.current.getBoundingClientRect();
    const w = popRef.current?.offsetWidth ?? POP_MIN_W;
    setPos(r.left + w > window.innerWidth - 8
      ? { top: r.bottom + 4, right: Math.max(8, window.innerWidth - r.right) }
      : { top: r.bottom + 4, left: r.left });
  }, [open, laps]);

  // Close on outside click, Escape, and scroll/resize (a fixed panel would otherwise detach from its chip).
  useEffect(() => {
    if (!open) return;
    const close = () => setOpen(false);
    const down = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!popRef.current?.contains(t) && !chipRef.current?.contains(t)) close();
    };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") { close(); chipRef.current?.focus(); } };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [open]);

  if (!slug) {
    return (
      <span className="ref-menu">
        <span className="chip chip--dim" title={waitingTip}>Ref · confirming track…</span>
      </span>
    );
  }

  const tip = stored
    ? [carTitle(stored.car), `recorded ${day(stored.recorded_at)}`, `session lap ${stored.lap_number}`, HINT].join("\n")
    : HINT;

  const run = async (fn: () => Promise<unknown>) => {
    setError(null);
    try { await fn(); setLaps(null); setOpen(false); onChanged(); } catch (e) { setError(refErrorText(e)); }
  };
  const openList = async () => {
    setError(null);
    try { setLaps(await fetchTrackLaps(slug)); } catch (e) { setError(refErrorText(e)); }
  };

  return (
    <span className="ref-menu">
      <button type="button" ref={chipRef} className={stored ? "chip" : "chip chip--dim"} title={tip}
        aria-haspopup="true" aria-expanded={open} onClick={() => setOpen(o => !o)}>
        {stored ? `Ref ${clock(stored.lap_time)} · ${stored.source}` : "No stored ref"} <span aria-hidden="true">▾</span>
      </button>
      {open && createPortal(
        <div ref={popRef} className="ref-menu__pop card" style={pos ?? { top: 0, left: 0, visibility: "hidden" }}>
          <button type="button" className="chip" onClick={openList}>Change…</button>
          {stored?.source === "pinned" && (
            <button type="button" className="chip" onClick={() => run(() => clearPin(slug))}>Clear pin</button>
          )}
          {laps && (
            <ul className="ref-menu__list" aria-label="Stored laps">
              {laps.length === 0 && <li className="text-muted">No stored laps on this track yet</li>}
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
        </div>,
        document.body,
      )}
      {error && <span className="chip chip--red" role="alert">{error}</span>}
    </span>
  );
}
