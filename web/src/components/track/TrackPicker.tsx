export type TrackListItem = { slug: string; name: string; coordinate_usability?: string };

// Chips beside the select (at most one status chip, plus an error):
//  - source "user": the user confirmed this track (it won't change under them);
//  - pending: the shown track is a pick waiting for the next session;
//  - provisional: the backend's still-unlocked auto-detected guess (it may switch).
// onAuto adds an "Auto-detect" first option that hands the track back to detection.
export function TrackPicker({ value, items, onPick, onAuto, provisional = false, source = null, pending = false, error = null }: {
  value: string | null; items: TrackListItem[]; onPick: (slug: string) => void; onAuto?: () => void;
  provisional?: boolean; source?: "user" | "detected" | null; pending?: boolean; error?: string | null;
}) {
  const select = (
    <select className="track-picker" value={value ?? ""} aria-label="Track"
      onChange={e => (e.target.value === "" ? onAuto?.() : onPick(e.target.value))}
      title={items.find(it => it.slug === value)?.name}>
      {onAuto ? <option value="">Auto-detect</option> : <option value="" disabled>Select track...</option>}
      {items.map(it => <option key={it.slug} value={it.slug}>{it.name}</option>)}
    </select>
  );
  const status = pending
    ? <span className="chip chip--dashed" title="Your pick applies when the next session starts">applies to next session</span>
    : source === "user"
      ? <span className="chip" title="You confirmed this track; detection will not change it">confirmed by you</span>
      : provisional
        ? <span className="chip chip--dashed" title="Best guess so far; may change until about a lap is driven">provisional</span>
        : null;
  if (!status && !error) return select;
  return (
    <span className="chip-row track-picker-row">
      {select}
      {status}
      {error && <span className="chip chip--red" role="alert">{error}</span>}
    </span>
  );
}
