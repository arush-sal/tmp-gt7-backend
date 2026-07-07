import { useState } from "react";
import type { HistoryStore } from "../../lib/historyStore";
import TelemetryGraph from "./TelemetryGraph";

interface Props {
  store: HistoryStore;
  mode: "panel" | "overlay";
}

// Minimal fixed graph set for this pass — full add/remove/reorder is a later phase.
const GRAPHS = [
  { key: "throttle-brake", title: "Throttle / Brake", channels: ["throttle", "brake"] as const, unit: "%" },
  { key: "speed", title: "Speed", channels: ["speed"] as const, unit: "km/h" },
];

/**
 * Hosts the live telemetry graphs in one of two layouts:
 *  - "panel": normal flow, lives in the left column (replaces the old accordion).
 *  - "overlay": floats absolutely over the center track-map canvas, translucent surface,
 *    so the map is still visible around it.
 */
export default function TelemetryPanel({ store, mode }: Props) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const toggle = (key: string) =>
    setCollapsed(prev => ({ ...prev, [key]: !prev[key] }));

  return (
    <div className={`telemetry-panel card ${mode === "overlay" ? "telemetry-panel--overlay" : ""}`}>
      <div className="telemetry-panel__header">
        <span className="heading">Telemetry</span>
      </div>

      <div className="telemetry-panel__list">
        {GRAPHS.map(g => {
          const isCollapsed = !!collapsed[g.key];
          return (
            <div key={g.key} className="telemetry-graph">
              <button
                className="telemetry-graph__tab"
                onClick={() => toggle(g.key)}
                aria-expanded={!isCollapsed}
              >
                <span>{isCollapsed ? "▸" : "▾"} {g.title}</span>
                {g.unit && <span className="telemetry-graph__unit-tag">{g.unit}</span>}
              </button>
              {!isCollapsed && (
                <TelemetryGraph
                  store={store}
                  channels={[...g.channels]}
                  label={g.title}
                  unit={g.unit}
                />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
