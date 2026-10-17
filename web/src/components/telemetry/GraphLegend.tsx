import type { Channel } from "../../lib/historyStore";
import { CHANNEL_COLOR, MEDIAN_COLOR, REFERENCE_COLOR } from "./graphColors";
import type { HoverReadout } from "./TelemetryGraph";

interface Props {
  variant: "single" | "multi";
  channels: Channel[];
  hidden: string[];                 // series keys currently off
  medianAvailable: boolean;         // false when no median data for this channel / not in analysis mode
  referenceAvailable: boolean;      // false when no reference data / not in analysis mode
  onToggle: (seriesKey: string) => void;
  hover?: HoverReadout | null;      // hovered per-series values (keyed by series label), shown inline
}

// A clickable legend. Single-series cards show actual/median/reference; multi-series cards show
// one chip per channel. Reference is disabled (no data) until the backend ships it. "current" is
// compulsory (always visible) so it renders as a static, non-interactive chip rather than a toggle.
// On hover, each chip appends the value at the cursor (lifted from the plot — see TelemetryGraph),
// so the readout never covers the line.
export default function GraphLegend({ variant, channels, hidden, medianAvailable, referenceAvailable, onToggle, hover = null }: Props) {
  // `key` is the toggle key; `seriesKey` is the uPlot series label the hovered value is stored under
  // ("current"/actual maps to the channel name).
  const chip = (key: string, label: string, color: string, seriesKey: string, disabled = false) => {
    const off = hidden.includes(key);
    const val = hover?.values[seriesKey];
    return (
      <button
        key={key}
        type="button"
        className={`chip ${off ? "chip--off" : ""}`.trim()}
        style={{ color }}
        aria-pressed={disabled ? undefined : !off}
        aria-disabled={disabled || undefined}
        disabled={disabled}
        title={`${label}${val != null ? ` ${val}` : ""}`}
        onClick={disabled ? undefined : () => onToggle(key)}
      >
        {label}{val != null ? ` ${val}` : ""}
      </button>
    );
  };

  const staticChip = (key: string, label: string, color: string, seriesKey: string) => {
    const val = hover?.values[seriesKey];
    return (
      <span key={key} className="chip chip--static" style={{ color }} title={`${label}${val != null ? ` ${val}` : ""}`}>
        {label}{val != null ? ` ${val}` : ""}
      </span>
    );
  };

  if (variant === "multi") {
    return <div className="graph-legend">{channels.map(c => chip(c, c, CHANNEL_COLOR[c], c))}</div>;
  }
  return (
    <div className="graph-legend">
      {staticChip("actual", "— current", CHANNEL_COLOR[channels[0]], channels[0])}
      {chip("median", "— median", MEDIAN_COLOR, "median", !medianAvailable)}
      {chip("reference", "— reference", REFERENCE_COLOR, "reference", !referenceAvailable)}
    </div>
  );
}
