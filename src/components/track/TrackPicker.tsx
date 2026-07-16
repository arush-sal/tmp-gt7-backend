export type TrackListItem = { slug: string; name: string; coordinate_usability?: string };

export function TrackPicker({ value, items, onPick }: {
  value: string | null; items: TrackListItem[]; onPick: (slug: string) => void;
}) {
  return (
    <select className="track-picker" value={value ?? ""} onChange={e => onPick(e.target.value)} aria-label="Track">
      <option value="" disabled>Select track...</option>
      {items.map(it => <option key={it.slug} value={it.slug}>{it.name}</option>)}
    </select>
  );
}
