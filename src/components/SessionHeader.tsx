import type { DashboardSnapshot } from "../contract/dashboard";
import type { SessionMeta, CornerAnalysis } from "../mock/snapshot";
import { formatLapTime, formatDelta } from "../lib/format";

interface Props {
  snapshot: DashboardSnapshot;
  meta: SessionMeta;
  predictedLapMs: number;
  selectedCorner: CornerAnalysis | null;
}

export default function SessionHeader({ snapshot, meta, predictedLapMs, selectedCorner }: Props) {
  const { position, lap } = snapshot;
  const deltaClass = lap.liveDelta <= 0 ? "text-green" : "text-red";

  return (
    <header className="session-header">
      <div className="header-bar">

        <div className="header-cell">
          <span className="label">Position</span>
          <span className="mono header-val">
            P{position.current}<span className="muted">/{position.total}</span>
          </span>
        </div>

        <div className="header-cell">
          <span className="label">Lap</span>
          <span className="mono header-val">
            {lap.number}<span className="muted">/{lap.total}</span>
          </span>
        </div>

        <div className="header-cell header-cell--wide">
          <span className="label">Track / Layout</span>
          <span className="header-val">{meta.trackName} — {meta.trackLayout}</span>
        </div>

        {selectedCorner ? (
          <>
            <div className="header-cell header-cell--corner">
              <span className="label label--red">Selected corner</span>
              <span className="header-val text-red">{selectedCorner.name}</span>
            </div>
            <div className="header-cell">
              <span className="label">Corner delta</span>
              <span className="mono header-val text-red">
                +{selectedCorner.delta.toFixed(3)}s
              </span>
            </div>
          </>
        ) : (
          <>
            <div className="header-cell">
              <span className="label">Live delta</span>
              <span className={`mono header-val ${deltaClass}`}>
                {formatDelta(lap.liveDelta)}
              </span>
            </div>
            <div className="header-cell">
              <span className="label">Predicted</span>
              <span className="mono header-val text-purple">
                {formatLapTime(predictedLapMs)}
              </span>
            </div>
          </>
        )}

        <div className="header-cell">
          <span className="label">Best lap</span>
          <span className={`mono header-val ${lap.bestLapStanding === "gridFastest" ? "text-purple" : "text-blue"}`}>
            {formatLapTime(lap.bestLapTime)}
          </span>
        </div>

      </div>
    </header>
  );
}
