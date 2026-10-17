export type TrackListItem = { slug: string; name: string; coordinate_usability?: string };

// provisional: the value is the backend's still-unlocked auto-detected guess (it may switch), shown
// as a muted chip beside the picker.
export function TrackPicker({ value, items, onPick, provisional = false }: {
  value: string | null; items: TrackListItem[]; onPick: (slug: string) => void; provisional?: boolean;
}) {
  const select = (
    <select className="track-picker" value={value ?? ""} onChange={e => onPick(e.target.value)} aria-label="Track">
      <option value="" disabled>Select track...</option>
      {items.map(it => <option key={it.slug} value={it.slug}>{it.name}</option>)}
    </select>
  );
  if (!provisional) return select;
  return (
    <span className="chip-row track-picker-row">
      {select}
      <span className="chip chip--dashed" title="Best guess so far; may change until about a lap is driven">provisional</span>
    </span>
  );
}
