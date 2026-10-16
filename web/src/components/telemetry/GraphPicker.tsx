import { CARD_CATALOG } from "./cardCatalog";

interface Props {
  shownKeys: string[];
  onAdd: (key: string) => void;
  onClose: () => void;
}

// A small "Custom Graphs" picker: catalog entries not already shown. needs-backend entries
// (steering) are listed but disabled so the gap is visible without being addable.
export default function GraphPicker({ shownKeys, onAdd, onClose }: Props) {
  const shown = new Set(shownKeys);
  const options = CARD_CATALOG.filter(c => !shown.has(c.key));
  return (
    <div className="graph-picker" role="dialog" aria-label="Custom Graphs">
      <div className="graph-picker__header">
        <span className="heading">Custom Graphs</span>
        <button type="button" className="graph-picker__close" onClick={onClose} aria-label="Close">×</button>
      </div>
      <ul className="graph-picker__list">
        {options.map(c => {
          const disabled = c.availability !== "available";
          return (
            <li key={c.key}>
              <button
                type="button"
                disabled={disabled}
                aria-disabled={disabled || undefined}
                onClick={disabled ? undefined : () => onAdd(c.key)}
                title={disabled ? "Needs backend support" : undefined}
              >
                {c.title}{disabled ? " (needs backend)" : ""}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
