import { useState } from "react";
import { mockCorners, mockCornerDetails } from "../mock/snapshot";
import type { CornerAnalysis } from "../mock/snapshot";
import { useSession } from "../hooks/useSession";
import { toLiveView } from "../lib/liveView";
import { subscribeLive, type LiveSource } from "../lib/liveSource";
import SessionHeader from "./SessionHeader";
import TrackMap from "./TrackMap";
import TelemetryAccordion from "./TelemetryAccordion";
import CornerDetailPanel from "./CornerDetailPanel";
import RightPanel from "./RightPanel";

export default function Dashboard({ source = subscribeLive }: { source?: LiveSource }) {
  const { snapshot, error } = useSession(undefined, source);
  const view = snapshot ? toLiveView(snapshot) : null;

  const [selectedCornerId, setSelectedCornerId] = useState<number | null>(null);
  const selectedCornerDetail: CornerAnalysis | null =
    selectedCornerId !== null ? (mockCornerDetails.find(c => c.id === selectedCornerId) ?? null) : null;
  const handleCornerSelect = (id: number | null) => setSelectedCornerId(id);

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
          {selectedCornerDetail !== null
            ? <CornerDetailPanel corner={selectedCornerDetail} onClear={() => setSelectedCornerId(null)} />
            : <TelemetryAccordion />}
        </div>

        <TrackMap
          corners={mockCorners}
          selectedCornerId={selectedCornerId}
          onCornerSelect={handleCornerSelect}
          selectedCornerDetail={selectedCornerDetail}
        />

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
