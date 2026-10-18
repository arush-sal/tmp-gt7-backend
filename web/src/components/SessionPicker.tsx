import type { SessionSummaryDTO } from "../contract/session";
import { nsToClock, parseDuration } from "../lib/format";

type Names = Record<string, string>;

function bestOf(s: SessionSummaryDTO): string {
  try { return s.best_lap ? nsToClock(parseDuration(s.best_lap)) : ""; } catch { return ""; } // malformed: omit
}
const trackOf = (s: SessionSummaryDTO, names: Names) => (s.track_slug ? names[s.track_slug] ?? s.track_slug : "");

// Short option text: "<track> · <car> · <best>" (+ " · live" while active); missing parts are omitted.
function baseLabel(s: SessionSummaryDTO, names: Names): string {
  const parts = [trackOf(s, names), s.car_name || "unknown car", bestOf(s)].filter(Boolean);
  return parts.join(" · ") + (s.status === "active" ? " · live" : "");
}

// sessionLabels maps session id -> short label; identical labels get " (2)", " (3)" by start time
// (oldest keeps the bare label) so entries stay distinguishable.
export function sessionLabels(sessions: SessionSummaryDTO[], names: Names): Map<string, string> {
  const seen = new Map<string, number>();
  const out = new Map<string, string>();
  // Start time, then id: a total order, so suffixes don't flip when a refetch returns equal-start
  // sessions in a different order.
  const byStart = (x: SessionSummaryDTO, y: SessionSummaryDTO) =>
    Date.parse(x.start_time) - Date.parse(y.start_time) || (x.id < y.id ? -1 : x.id > y.id ? 1 : 0);
  for (const s of [...sessions].sort(byStart)) {
    const base = baseLabel(s, names);
    const n = (seen.get(base) ?? 0) + 1;
    seen.set(base, n);
    out.set(s.id, n > 1 ? `${base} (${n})` : base);
  }
  return out;
}

// Full details for the tooltip / select title: local date/time, track, car, best lap, laps, status.
export function sessionTitle(s: SessionSummaryDTO, names: Names): string {
  const laps = s.total_laps > 0 ? `${s.current_lap}/${s.total_laps} laps` : `${s.current_lap} laps`;
  return [new Date(s.start_time).toLocaleString(), trackOf(s, names) || "no confirmed track",
    s.car_name || "unknown car", bestOf(s) ? `best ${bestOf(s)}` : "no best lap", laps, s.status].join(" · ");
}

interface Props {
  sessions: SessionSummaryDTO[];
  trackNames: Names;               // track slug -> display name (unknown slugs show as the slug)
  error: boolean;
  selectedId: string | null;       // null = live
  replay: boolean;                 // live feed is a replay of a stored session
  onSelect: (id: string | null) => void;
  onOpen: () => void;              // refresh the list when the picker is used
}

export default function SessionPicker({ sessions, trackNames, error, selectedId, replay, onSelect, onOpen }: Props) {
  const known = selectedId === null || sessions.some(s => s.id === selectedId);
  const shown = sessions.find(x => x.id === selectedId);
  const fullLabel = selectedId === null ? "Live" : shown ? sessionTitle(shown, trackNames) : selectedId;
  const labels = sessionLabels(sessions, trackNames);
  return (
    <div className="session-picker">
      <select
        aria-label="Session"
        title={fullLabel}
        className="header-select"
        value={selectedId ?? ""}
        onFocus={onOpen}
        onChange={e => onSelect(e.target.value || null)}
      >
        <option value="">Live</option>
        {!known && <option value={selectedId!}>{selectedId}</option>}
        {sessions.map(s => <option key={s.id} value={s.id} title={sessionTitle(s, trackNames)}>{labels.get(s.id)}</option>)}
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
