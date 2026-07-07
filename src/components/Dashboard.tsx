import { useState } from "react";
import { mockCorners, mockCornerDetails } from "../mock/snapshot";
import type { CornerAnalysis } from "../mock/snapshot";
import { useSession } from "../hooks/useSession";
import { useHistory } from "../hooks/useHistory";
import { toLiveView } from "../lib/liveView";
import { subscribeLive, type LiveSource } from "../lib/liveSource";
import SessionHeader from "./SessionHeader";
import TrackMap from "./TrackMap";
import TelemetryPanel from "./telemetry/TelemetryPanel";
import FloatingPanel from "./telemetry/FloatingPanel";
import CornerDetailPanel from "./CornerDetailPanel";
import RightPanel from "./RightPanel";

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
        <div className="center-column">
          <TrackMap
            corners={mockCorners}
            selectedCornerId={selectedCornerId}
            onCornerSelect={handleCornerSelect}
            selectedCornerDetail={selectedCornerDetail}
          />
          <FloatingPanel
            key="telemetry"
            storageKey="gt7:telemetry-geom"
            defaultGeom={{ x: 12, y: 12, width: 380, height: 640 }}
            dragHandleClassName="telemetry-panel__header"
          >
            <TelemetryPanel store={history} />
          </FloatingPanel>
          {selectedCornerDetail !== null && (
            <FloatingPanel
              key="corner"
              storageKey="gt7:corner-geom"
              defaultGeom={{ x: 400, y: 12, width: 320, height: 360 }}
              dragHandleClassName="corner-detail__header"
            >
              <CornerDetailPanel corner={selectedCornerDetail} onClear={() => setSelectedCornerId(null)} />
            </FloatingPanel>
          )}
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
