import type { SessionSummaryDTO } from "../contract/session";
import { nsToClock, parseDuration } from "../lib/format";

// Label: local date/time · car · best lap (when known) · laps.
export function sessionLabel(s: SessionSummaryDTO): string {
  const laps = s.total_laps > 0 ? `${s.current_lap}/${s.total_laps} laps` : `${s.current_lap} laps`;
  const when = new Date(s.start_time).toLocaleString();
  let best = "";
  try { if (s.best_lap) best = ` · ${nsToClock(parseDuration(s.best_lap))}`; } catch { /* malformed duration: omit */ }
  return `${when} · ${s.car_name || "unknown car"}${best} · ${laps}${s.status === "active" ? " · active" : ""}`;
}

interface Props {
  sessions: SessionSummaryDTO[];
  error: boolean;
  selectedId: string | null;       // null = live
  replay: boolean;                 // live feed is a replay of a stored session
  onSelect: (id: string | null) => void;
  onOpen: () => void;              // refresh the list when the picker is used
}

export default function SessionPicker({ sessions, error, selectedId, replay, onSelect, onOpen }: Props) {
  const known = selectedId === null || sessions.some(s => s.id === selectedId);
  return (
    <div className="session-picker">
      <select
        aria-label="Session"
        className="header-select"
        value={selectedId ?? ""}
        onFocus={onOpen}
        onChange={e => onSelect(e.target.value || null)}
      >
        <option value="">Live</option>
        {!known && <option value={selectedId!}>{selectedId}</option>}
        {sessions.map(s => <option key={s.id} value={s.id}>{sessionLabel(s)}</option>)}
      </select>
      {selectedId !== null && (
        <>
          <span className="chip chip--amber" role="status">Viewing stored session</span>
          <button type="button" className="chip" onClick={() => onSelect(null)}>Back to live</button>
        </>
      )}
      {replay && (
        <span className="chip chip--amber" role="status"
          title="The live feed is an in-game replay of an already-stored session; the laps shown are the stored originals.">
          REPLAY · stored session
        </span>
      )}
      {error && <span className="chip chip--red" role="alert">couldn't load sessions</span>}
    </div>
  );
}
