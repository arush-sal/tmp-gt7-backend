export function ConfidenceBadges({ coordinate, boundary, trackConfidence, mismatch }: {
  coordinate?: string; boundary?: string; trackConfidence?: string | null; mismatch: boolean;
}) {
  if (!import.meta.env.DEV) return null; // REMOVE-BEFORE-V1: verbose dev-only tier readout
  return (
    <div className="confidence-badges" data-testid="confidence-badges">
      {coordinate && <span className="chip">{coordinate}</span>}
      {boundary && <span className="chip">{boundary}</span>}
      {trackConfidence && <span className="chip">detect: {trackConfidence}</span>}
      {mismatch && <span className="chip chip--red">mismatch suspected</span>}
    </div>
  );
}
