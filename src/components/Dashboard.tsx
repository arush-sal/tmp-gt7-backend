import { useState } from "react";
import { mockCorners, mockCornerDetails } from "../mock/snapshot";
import type { CornerAnalysis } from "../mock/snapshot";
import { useSession } from "../hooks/useSession";
import { useHistory } from "../hooks/useHistory";
import { toLiveView } from "../lib/liveView";
import { subscribeLive, type LiveSource } from "../lib/liveSource";
import SessionHeader from "./SessionHeader";
import TrackMap from "./TrackMap";
import TelemetryAccordion from "./TelemetryAccordion";
import TelemetryPanel from "./telemetry/TelemetryPanel";
import CornerDetailPanel from "./CornerDetailPanel";
import RightPanel from "./RightPanel";

type TelemetryLayoutMode = "panel" | "overlay";
const LAYOUT_KEY = "gt7:telemetry-layout";

function loadLayoutMode(): TelemetryLayoutMode {
  try {
    const v = localStorage.getItem(LAYOUT_KEY);
    return v === "overlay" ? "overlay" : "panel";
  } catch {
    return "panel"; // storage disabled/blocked — non-fatal, fall back to panel
  }
}

function saveLayoutMode(mode: TelemetryLayoutMode): void {
  try {
    localStorage.setItem(LAYOUT_KEY, mode);
  } catch {
    // storage disabled/full — non-fatal, just won't persist.
  }
}

export default function Dashboard({ source = subscribeLive }: { source?: LiveSource }) {
  const { snapshot, error } = useSession(undefined, source);
  const view = snapshot ? toLiveView(snapshot) : null;
  // Independent live subscription (per the plan: fan-out to a single subscription is a
  // later pass) — feeds the graph buffer only, reuses the same injectable `source` as
  // useSession so tests (and a future single-subscription refactor) see one seam.
  const history = useHistory(source);

  const [selectedCornerId, setSelectedCornerId] = useState<number | null>(null);
  const selectedCornerDetail: CornerAnalysis | null =
    selectedCornerId !== null ? (mockCornerDetails.find(c => c.id === selectedCornerId) ?? null) : null;
  const handleCornerSelect = (id: number | null) => setSelectedCornerId(id);

  const [layoutMode, setLayoutMode] = useState<TelemetryLayoutMode>(loadLayoutMode);
  const toggleLayoutMode = () =>
    setLayoutMode(prev => {
      const next: TelemetryLayoutMode = prev === "panel" ? "overlay" : "panel";
      saveLayoutMode(next);
      return next;
    });

  return (
    <>
      {(!snapshot || error || snapshot.status === "idle") && (
        <div className="conn-banner">
          {error
            ? `stream error: ${error.message}`
            : !snapshot
              ? "connecting — waiting for telemetry…"
              : "waiting for telemetry — start a session in GT7"}
        </div>
      )}

      <SessionHeader view={view} selectedCorner={selectedCornerDetail} />

      <div className="dashboard-body">
        <div className="left-panel">
          <button
            className="toggle telemetry-layout-toggle"
            onClick={toggleLayoutMode}
            title="Switch telemetry layout"
          >
            ⧉ {layoutMode === "panel" ? "Floating" : "Panel"} view
          </button>

          {selectedCornerDetail !== null ? (
            <CornerDetailPanel corner={selectedCornerDetail} onClear={() => setSelectedCornerId(null)} />
          ) : layoutMode === "panel" ? (
            <TelemetryPanel store={history} mode="panel" />
          ) : (
            <TelemetryAccordion />
          )}
        </div>

        <div className="center-column">
          <TrackMap
            corners={mockCorners}
            selectedCornerId={selectedCornerId}
            onCornerSelect={handleCornerSelect}
            selectedCornerDetail={selectedCornerDetail}
          />
          {layoutMode === "overlay" && <TelemetryPanel store={history} mode="overlay" />}
        </div>

        <div className="right-panel-wrap">
          <RightPanel
            view={view}
            corners={mockCorners}
            selectedCornerId={selectedCornerId}
            onCornerSelect={handleCornerSelect}
            selectedCornerDetail={selectedCornerDetail}
          />
        </div>
      </div>
    </>
  );
}
