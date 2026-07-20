import type { LiveView } from "../lib/liveView";
import type { Corner, CornerAnalysis } from "../contract/corner";
import type { SectorTime } from "../lib/sectorTimer";
import { PLACEHOLDER, msToSectorTime, msToSectorDelta } from "../lib/format";

interface Props {
  view: LiveView | null;
  corners: Corner[];
  selectedCornerId: number | null;
  onCornerSelect: (id: number | null) => void;
  selectedCornerDetail: CornerAnalysis | null;
  usedPerLap?: number | null;
  lapsLeft?: number | null;
  sectorTimes?: SectorTime[] | null;
}

export default function RightPanel({
  view, corners, selectedCornerId, onCornerSelect, selectedCornerDetail,
  usedPerLap = null, lapsLeft = null, sectorTimes = null,
}: Props) {
  const opportunities = [...corners]
    .filter(c => c.delta > 0.05)
    .sort((a, b) => b.delta - a.delta)
    .slice(0, 3);

  const activeCoach = selectedCornerDetail ?? null;
  const hasBest = !!view && view.bestLap !== PLACEHOLDER;

  return (
    <div className="right-panel">

      {/* ── Sector times ── */}
      <div className="section">
        <div className="section__row" style={{ marginBottom: 6 }}>
          <span className="label">Sector Times</span>
          {sectorTimes && <span className="label text-muted" title="Derived from distance-based sector labels, not exact sector-line geometry">approx</span>}
        </div>
        {sectorTimes ? (
          sectorTimes.map(st => (
            <div className="data-row" key={st.sector}>
              <span className="text-muted">{st.sector}</span>
              <span className="mono">
                {msToSectorTime(st.timeMs)}
                {st.deltaMs !== null && (
                  <span className={st.deltaMs > 0 ? "text-red" : st.deltaMs < 0 ? "text-blue" : "text-muted"}>
                    {" "}({msToSectorDelta(st.deltaMs)})
                  </span>
                )}
              </span>
            </div>
          ))
        ) : (
          <div className="data-row"><span className="text-muted">sector data pending</span><span className="mono text-muted">—</span></div>
        )}
      </div>

      {/* ── Lap history ── */}
      <div className="section">
        <div className="label" style={{ marginBottom: 6 }}>Lap History</div>
        <div className="data-row">
          <span className="text-muted">Current</span>
          <span className="mono text-muted">{view ? view.currentLapTime : "—"}</span>
        </div>
        <div className="data-row">
          <span className="text-muted">Previous</span>
          <span className="mono">{view ? view.lastLap : "—"}</span>
        </div>
        <div className="data-row" style={{ borderBottom: "none" }}>
          <span className="text-muted">Best lap</span>
          <span className={`mono ${hasBest ? "text-blue lap-best-hl" : "text-muted"}`}>{view ? view.bestLap : "—"}</span>
        </div>
      </div>

      {/* ── Top opportunities ── */}
      <div className="section section--hi">
        <div className="label" style={{ marginBottom: 8 }}>Top Opportunities</div>
        {opportunities.length === 0 && (
          <div className="data-row" style={{ borderBottom: "none" }}>
            <span className="text-muted">coaching pending</span>
            <span className="mono text-muted">—</span>
          </div>
        )}
        {opportunities.map(c => {
          const isActive = c.id === selectedCornerId;
          return (
            <button
              key={c.id}
              className={`opp-row ${isActive ? "opp-row--active" : ""}`}
              onClick={() => onCornerSelect(c.id === selectedCornerId ? null : c.id)}
            >
              <span className={isActive ? "text-red" : "body-text"}>
                {isActive ? "▸ " : ""}{c.name}
              </span>
              <span className="pill-red mono">+{c.delta.toFixed(3)}s</span>
            </button>
          );
        })}
      </div>

      {/* ── AI Coaching card ── */}
      <div className="section section--coaching">
        <div className="section__row" style={{ marginBottom: 6 }}>
          <span className="label">
            {activeCoach ? `AI Coaching — ${activeCoach.name}` : "AI Coaching Card"}
          </span>
          <span className="confidence-badge confidence-badge--high">
            {activeCoach ? activeCoach.coaching.confidence : "pending"}
          </span>
        </div>

        {activeCoach ? (
          <>
            <div className="coaching-title">{activeCoach.name}</div>
            <div className="label label--red" style={{ marginTop: 4, marginBottom: 2 }}>Problem</div>
            <p className="body-text">{activeCoach.coaching.problem}</p>
            <div className="label label--green" style={{ marginTop: 4, marginBottom: 2 }}>Action</div>
            <p className="body-text">{activeCoach.coaching.action}</p>
            <div className="label text-muted" style={{ marginTop: 4, marginBottom: 2 }}>Evidence</div>
            <p className="body-text text-muted">{activeCoach.coaching.evidence}</p>
          </>
        ) : (
          <>
            <div className="coaching-title text-muted">Coaching pending</div>
            <p className="body-text text-muted">Corner-level coaching is not available yet.</p>
          </>
        )}

        <div className="data-row" style={{ borderBottom: "none", marginTop: 6 }}>
          <span className="text-muted">Expected gain</span>
          <span className="mono text-green" style={{ fontWeight: 700, fontSize: 14 }}>
            {activeCoach ? `${activeCoach.coaching.expectedGain.toFixed(2)}s` : "—"}
          </span>
        </div>
      </div>

      {/* ── Tyre + Fuel row ── */}
      <div style={{ display: "flex", gap: 8 }}>

        {/* Tyre — each bar is filled with the tyre's current temperature colour band. */}
        <div className="section" style={{ flex: 1.5 }}>
          <div className="section__row" style={{ marginBottom: 8 }}>
            <span className="label">Tyre · temp</span>
          </div>
          <div style={{ display: "flex", justifyContent: "space-between" }}>
            {(["fl","fr","rl","rr"] as const).map(pos => {
              const t = view ? view.tyreTemps[pos] : { value: "—", tone: "none" as const };
              return (
                <div key={pos} className="tyre-col">
                  <span className="label" style={{ fontSize: 9 }}>{pos.toUpperCase()}</span>
                  <div className="tyre-bar"><div className={`tyre-fill tyre-fill--${t.tone}`} /></div>
                  <span className={`mono tyre-${t.tone}`} style={{ fontSize: 9 }}>{t.value}</span>
                </div>
              );
            })}
          </div>
        </div>

        {/* Fuel */}
        <div className="section" style={{ flex: 1 }}>
          <div className="label" style={{ marginBottom: 5 }}>Fuel</div>
          <div style={{ display: "flex", alignItems: "baseline", marginBottom: 4 }}>
            <span className={`mono fuel-${view ? view.fuelTone : "none"}`} style={{ fontSize: 15, fontWeight: 600 }}>
              {view ? view.fuelPct : "—"}
            </span>
          </div>
          <div className="data-row">
            <span className="text-muted">Used/lap</span>
            <span className="mono text-muted">{usedPerLap !== null ? `${usedPerLap.toFixed(1)}%` : "—"}</span>
          </div>
          <div className="data-row" style={{ borderBottom: "none" }}>
            <span className="text-muted">Laps left</span>
            <span className="mono text-muted">{lapsLeft !== null ? lapsLeft.toFixed(1) : "—"}</span>
          </div>
        </div>

      </div>
    </div>
  );
}
