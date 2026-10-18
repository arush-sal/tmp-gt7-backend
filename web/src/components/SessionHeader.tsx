import type { ReactNode } from "react";
import type { LiveView } from "../lib/liveView";
import type { CornerAnalysis } from "../contract/corner";
import { PLACEHOLDER } from "../lib/format";
import Tooltip from "./Tooltip";

interface Props {
  view: LiveView | null;
  selectedCorner: CornerAnalysis | null;
  trackPicker?: ReactNode;
  lapSelector?: ReactNode;
  sessionPicker?: ReactNode;
  hideLive?: boolean; // analysis mode: drop the live-only cells (Position, Lap delta, Predicted)
}

export default function SessionHeader({ view, selectedCorner, trackPicker, lapSelector, sessionPicker, hideLive = false }: Props) {
  const ph = "—";
  const deltaClass =
    view?.deltaTone === "blue" ? "text-blue" : view?.deltaTone === "red" ? "text-red" : "text-muted";
  const hasBest = !!view && view.bestLap !== PLACEHOLDER;

  return (
    <header className="session-header">
      <div className="header-bar">

        {!hideLive && (
          <div className="header-cell">
            <span className="label">Position</span>
            <span className="mono header-val">{view ? view.position : ph}</span>
          </div>
        )}

        {sessionPicker && (
          <div className="header-cell header-cell--picker">
            <span className="label">Session</span>
            {sessionPicker}
          </div>
        )}

        <div className="header-cell">
          <div className="lap-cell-row">
            <span className="label">Lap</span>
            <span className="mono header-val">{view ? view.lap : ph}</span>
          </div>
          {lapSelector}
        </div>

        <div className="header-cell header-cell--picker header-cell--track">
          <span className="label">Track / Layout</span>
          {trackPicker ?? <span className="header-val text-muted">{ph}</span>}
        </div>

        {selectedCorner ? (
          <>
            <div className="header-cell header-cell--corner">
              <span className="label label--red">Selected corner</span>
              <span className="header-val text-red">{selectedCorner.name}</span>
            </div>
            <div className="header-cell">
              <span className="label">Corner delta</span>
              <span className="mono header-val text-red">+{selectedCorner.delta.toFixed(3)}s</span>
            </div>
          </>
        ) : hideLive ? null : (
          <>
            <div className="header-cell">
              <Tooltip text="Live gap to your best lap at this point on track (- ahead, + behind)."><span className="label">Lap delta</span></Tooltip>
              <span className={`mono header-val ${deltaClass}`}>{view ? view.delta : ph}</span>
            </div>
            <div className="header-cell">
              <Tooltip text="Your ideal lap — the sum of your fastest sector times this session."><span className="label">Predicted</span></Tooltip>
              <span className="mono header-val text-purple">{view ? view.predicted : ph}</span>
            </div>
          </>
        )}

        <div className="header-cell">
          <span className="label">Best lap</span>
          <span className={`mono header-val ${hasBest ? "text-blue" : "text-muted"}`}>{view ? view.bestLap : ph}</span>
        </div>

      </div>
    </header>
  );
}
