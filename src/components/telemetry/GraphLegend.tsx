import type { Channel } from "../../lib/historyStore";
import { CHANNEL_COLOR, MEDIAN_COLOR, REFERENCE_COLOR } from "./graphColors";

interface Props {
  variant: "single" | "multi";
  channels: Channel[];
  hidden: string[];                 // series keys currently off
  referenceAvailable: boolean;      // false until backend RKB
  onToggle: (seriesKey: string) => void;
}

// A clickable legend. Single-series cards show actual/median/reference; multi-series cards show
// one chip per channel. Reference is disabled (no data) until the backend ships it. "current" is
// compulsory (always visible) so it renders as a static, non-interactive chip rather than a toggle.
export default function GraphLegend({ variant, channels, hidden, referenceAvailable, onToggle }: Props) {
  const chip = (key: string, text: string, color: string, disabled = false) => {
    const off = hidden.includes(key);
    return (
      <button
        key={key}
        type="button"
        className={`chip ${off ? "chip--off" : ""}`.trim()}
        style={{ color }}
        aria-pressed={disabled ? undefined : !off}
        aria-disabled={disabled || undefined}
        disabled={disabled}
        onClick={disabled ? undefined : () => onToggle(key)}
      >
        {text}
      </button>
    );
  };

  const staticChip = (key: string, text: string, color: string) => (
    <span key={key} className="chip chip--static" style={{ color }}>
      {text}
    </span>
  );

  if (variant === "multi") {
    return <div className="graph-legend">{channels.map(c => chip(c, c, CHANNEL_COLOR[c]))}</div>;
  }
  return (
    <div className="graph-legend">
      {staticChip("actual", "— current", CHANNEL_COLOR[channels[0]])}
      {chip("median", "— median", MEDIAN_COLOR)}
      {chip("reference", "— reference", REFERENCE_COLOR, !referenceAvailable)}
    </div>
  );
}
