import type { CornerAnalysis } from "../contract/corner";

interface Props {
  corner: CornerAnalysis;
  onClear: () => void;
}

function DeltaRow({ label, value, unit = "", flip = false }: {
  label: string;
  value: number;
  unit?: string;
  flip?: boolean; // when true, positive = good (e.g. exit speed gain)
}) {
  const bad = flip ? value < 0 : value > 0;
  const cls = bad ? "text-red" : value === 0 ? "text-muted" : "text-green";
  const sign = value > 0 ? "+" : "";
  return (
    <div className="data-row">
      <span className="text-2">{label}</span>
      <span className={`mono ${cls}`}>{sign}{value.toFixed(value % 1 === 0 ? 0 : 3)}{unit}</span>
    </div>
  );
}

export default function CornerDetailPanel({ corner, onClear }: Props) {
  return (
    <div className="corner-detail card">
      {/* Header */}
      <div className="corner-detail__header">
        <span className="heading">{corner.name}</span>
        <button className="clear-btn" onClick={onClear}>✕ clear</button>
      </div>
      <div className="label text-muted" style={{ marginBottom: 10 }}>
        corner detail mode
      </div>

      {/* Total delta */}
      <div className="total-delta-box">
        <span className="label label--red">Total loss</span>
        <span className="mono text-red" style={{ fontSize: 22, fontWeight: 700 }}>
          +{corner.delta.toFixed(3)}s
        </span>
      </div>

      {/* Delta breakdown */}
      <div className="section" style={{ marginTop: 8 }}>
        <div className="label" style={{ marginBottom: 5 }}>Delta breakdown</div>
        <DeltaRow label="Brake timing"   value={corner.brakeDelta}       unit="s" />
        <DeltaRow label="Entry speed"    value={corner.entrySpeedDelta}  unit=" km/h" />
        <DeltaRow label="Apex speed"     value={corner.apexSpeedDelta}   unit=" km/h" />
        <DeltaRow label="Exit speed"     value={corner.exitSpeedDelta}   unit=" km/h" flip />
        <div style={{ height: 0 }} />
      </div>

      {/* Reference vs actual */}
      <div className="section" style={{ marginTop: 7 }}>
        <div className="label" style={{ marginBottom: 5 }}>Reference vs actual</div>
        <div className="data-row">
          <span className="text-2">Brake point</span>
          <span className="mono">
            {corner.refBrakePoint}m →{" "}
            <span className="text-red">{corner.actualBrakePoint}m</span>
          </span>
        </div>
        <div className="data-row">
          <span className="text-2">Apex speed</span>
          <span className="mono">
            {corner.refApexSpeed} →{" "}
            <span className="text-red">{corner.actualApexSpeed} km/h</span>
          </span>
        </div>
        <div className="data-row" style={{ borderBottom: "none" }}>
          <span className="text-2">Throttle pickup</span>
          <span className="mono text-red">+{corner.throttlePickupDelta.toFixed(1)}s late</span>
        </div>
      </div>
    </div>
  );
}
