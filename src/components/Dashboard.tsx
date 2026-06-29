import { useState } from "react";
import {
  mockSnapshot, mockCorners, mockCornerDetails,
  mockMeta, predictedLapMs,
} from "../mock/snapshot";
import type { CornerAnalysis } from "../mock/snapshot";
import SessionHeader from "./SessionHeader";
import TrackMap from "./TrackMap";
import TelemetryAccordion from "./TelemetryAccordion";
import CornerDetailPanel from "./CornerDetailPanel";
import RightPanel from "./RightPanel";

// Mock data for now. This is the line that swaps for the real useSession hook:
//   const { snapshot } = useSession("/api/snapshot");
const snapshot = mockSnapshot;

export default function Dashboard() {
  const [selectedCornerId, setSelectedCornerId] = useState<number | null>(null);

  const selectedCornerDetail: CornerAnalysis | null =
    selectedCornerId !== null
      ? (mockCornerDetails.find(c => c.id === selectedCornerId) ?? null)
      : null;

  const handleCornerSelect = (id: number | null) => setSelectedCornerId(id);

  return (
    <>
      <SessionHeader
        snapshot={snapshot}
        meta={mockMeta}
        predictedLapMs={predictedLapMs}
        selectedCorner={selectedCornerDetail}
      />

      <div className="dashboard-body">
        {/* Left panel — telemetry accordion OR corner detail */}
        <div className="left-panel">
          {selectedCornerDetail !== null ? (
            <CornerDetailPanel
              corner={selectedCornerDetail}
              onClear={() => setSelectedCornerId(null)}
            />
          ) : (
            <TelemetryAccordion />
          )}
        </div>

        {/* Center — track map */}
        <TrackMap
          corners={mockCorners}
          selectedCornerId={selectedCornerId}
          onCornerSelect={handleCornerSelect}
          selectedCornerDetail={selectedCornerDetail}
        />

        {/* Right panel */}
        <div className="right-panel-wrap">
          <RightPanel
            snapshot={snapshot}
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
