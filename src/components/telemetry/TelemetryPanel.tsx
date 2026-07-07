import { useState } from "react";
import type { HistoryStore } from "../../lib/historyStore";
import TelemetryGraph from "./TelemetryGraph";

interface Props {
  store: HistoryStore;
}

// One metric per card (accordion-style) — full add/remove/reorder is a later phase.
const GRAPHS = [
  { key: "throttle", title: "Throttle", channels: ["throttle"] as const, unit: "%" },
  { key: "brake", title: "Brake", channels: ["brake"] as const, unit: "%" },
  { key: "speed", title: "Speed", channels: ["speed"] as const, unit: "km/h" },
];

/**
 * Hosts the live telemetry graphs as single-metric accordion-style cards. Always floats
 * (via the caller's FloatingPanel) over the center track-map canvas — the old fixed
 * "panel"/"overlay" layout modes are gone, this is the only layout now.
 */
export default function TelemetryPanel({ store }: Props) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const toggle = (key: string) =>
    setCollapsed(prev => ({ ...prev, [key]: !prev[key] }));

  return (
    <div className="telemetry-panel">
      <div className="telemetry-panel__header">
        <span className="heading">Telemetry</span>
      </div>

      <div className="telemetry-panel__list">
        {GRAPHS.map(g => {
          const isCollapsed = !!collapsed[g.key];
          return (
            <div
              key={g.key}
              className={`telemetry-graph accordion-item ${!isCollapsed ? "telemetry-graph--expanded" : ""}`}
            >
              <button
                type="button"
                className="telemetry-graph__tab accordion-tab"
                onClick={() => toggle(g.key)}
                aria-expanded={!isCollapsed}
                aria-controls={`tg-${g.key}`}
              >
                <span>{isCollapsed ? "▸" : "▾"} {g.title}</span>
                {g.unit && <span className="telemetry-graph__unit-tag">{g.unit}</span>}
              </button>
              {!isCollapsed && (
                <>
                  <TelemetryGraph
                    store={store}
                    channels={[...g.channels]}
                    label={g.title}
                    unit={g.unit}
                    id={`tg-${g.key}`}
                  />
                  <div className="graph-legend">
                    <span className="chip chip--green chip--dim" title="no reference lap set yet">
                      — reference
                    </span>
                    <span className="chip">— current</span>
                    <span className="chip chip--dashed chip--dim" title="needs multiple laps">
                      ·· median
                    </span>
                  </div>
                </>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
