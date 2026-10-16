import type { PeaksDTO } from "../contract/session";
import { peakDeltaColor } from "../lib/traceColor";
import Tooltip from "./Tooltip";

interface Props {
  data: PeaksDTO | null;
  loading: boolean;
  error: boolean;
  reference: "predicted" | number;          // current reference selection
  onReferenceChange: (ref: "predicted" | number) => void;
  lapOptions: number[];                      // real lap numbers selectable as reference B
  open: boolean;                             // collapsed/expanded (controlled — drives on-track symbols)
  onToggleOpen: () => void;
  // Cross-highlight with the canvas markers (keyed by ExtremumDTO.index).
  hoveredPeak?: number | null;
  pinnedPeak?: number | null;
  onHoverPeak?: (index: number | null) => void;
  onPinPeak?: (index: number) => void;
}

// Signed delta in seconds, e.g. "+0.097s" / "-0.198s"; blank when the reference is unusable.
function fmtDelta(ns: number | null): string {
  if (ns == null) return "—";
  const s = ns / 1e9;
  return `${s >= 0 ? "+" : ""}${s.toFixed(3)}s`;
}

// Speed Peaks & Valleys — the significant speed crests/apex slow-points of the selected lap, each
// with its time delta vs a chosen reference. Collapsible; renders inside RightPanel.
export default function PeaksTable({
  data, loading, error, reference, onReferenceChange, lapOptions, open, onToggleOpen,
  hoveredPeak = null, pinnedPeak = null, onHoverPeak, onPinPeak,
}: Props) {
  const refReliable = data?.reference_reliable ?? false;
  const points = data?.points ?? [];

  return (
    <div className="section">
      <div className="section__row" style={{ marginBottom: 6 }}>
        <Tooltip text="Time gained/lost vs the reference at this point — red (+) = slower/behind, green (-) = faster/ahead.">
          <button
            type="button"
            className="peaks__toggle label"
            onClick={onToggleOpen}
            aria-expanded={open}
            style={{ background: "none", border: "none", padding: 0 }}
          >
            {open ? "▾" : "▸"} Speed Peaks &amp; Valleys
          </button>
        </Tooltip>
        <select
          aria-label="Peaks reference"
          className="header-select"
          value={String(reference)}
          onChange={e => onReferenceChange(e.target.value === "predicted" ? "predicted" : Number(e.target.value))}
        >
          <option value="predicted">vs predicted</option>
          {lapOptions.map(n => <option key={n} value={n}>vs lap {n}</option>)}
        </select>
      </div>

      {open && (
        <>
          {loading && <div className="data-row"><span className="text-muted">loading…</span><span className="mono text-muted">—</span></div>}
          {error && <div className="data-row"><span className="text-red">couldn't load peaks</span><span className="mono text-muted">—</span></div>}
          {data && !data.reliable && data.reason && (
            <div className="data-row"><span className="text-muted" title={data.reason}>unreliable trace: {data.reason}</span></div>
          )}
          {data && !refReliable && data.reference_reason && (
            <div className="data-row"><span className="text-muted" title={data.reference_reason}>no reliable reference: {data.reference_reason}</span></div>
          )}
          {data && points.length === 0 && !loading && (
            <div className="data-row"><span className="text-muted">no peaks detected</span><span className="mono text-muted">—</span></div>
          )}
          {points.map(p => {
            const active = p.index === hoveredPeak || p.index === pinnedPeak;
            return (
              <div
                className="data-row"
                key={p.index}
                onMouseEnter={() => onHoverPeak?.(p.index)}
                onMouseLeave={() => onHoverPeak?.(null)}
                onClick={() => onPinPeak?.(p.index)}
                style={{ cursor: "pointer", background: active ? "rgba(177,79,255,0.14)" : undefined }}
              >
                <span className={p.index === pinnedPeak ? "" : "text-muted"}>
                  {p.kind === "peak" ? "▲" : "▼"} {Math.round(p.distance)}m
                </span>
                <span className="mono">
                  {Math.round(p.speed)} km/h
                  {refReliable && (
                    <span style={{ color: peakDeltaColor(p.delta_ns) }}> {fmtDelta(p.delta_ns)}</span>
                  )}
                </span>
              </div>
            );
          })}
        </>
      )}
    </div>
  );
}
