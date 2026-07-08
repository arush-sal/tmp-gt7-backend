import type { Channel } from "../../lib/historyStore";

interface Props {
  variant: "single" | "multi";
  channels: Channel[];
  hidden: string[];                 // series keys currently off
  referenceAvailable: boolean;      // false until backend RKB
  onToggle: (seriesKey: string) => void;
}

// A clickable legend. Single-series cards show actual/median/reference; multi-series cards show
// one chip per channel. Reference is disabled (no data) until the backend ships it.
export default function GraphLegend({ variant, channels, hidden, referenceAvailable, onToggle }: Props) {
  const chip = (key: string, text: string, extra = "", disabled = false) => {
    const off = hidden.includes(key);
    return (
      <button
        key={key}
        type="button"
        className={`chip ${extra} ${off ? "chip--off" : ""}`.trim()}
        aria-pressed={disabled ? undefined : !off}
        aria-disabled={disabled || undefined}
        disabled={disabled}
        onClick={disabled ? undefined : () => onToggle(key)}
      >
        {text}
      </button>
    );
  };

  if (variant === "multi") {
    return <div className="graph-legend">{channels.map(c => chip(c, c))}</div>;
  }
  return (
    <div className="graph-legend">
      {chip("actual", "— current")}
      {chip("median", "·· median", "chip--dashed chip--dim")}
      {chip("reference", "— reference", "chip--dim", !referenceAvailable)}
    </div>
  );
}
