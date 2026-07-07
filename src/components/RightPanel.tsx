import type { LiveView } from "../lib/liveView";
import type { Corner, CornerAnalysis } from "../mock/snapshot";

interface Props {
  view: LiveView | null;
  corners: Corner[];
  selectedCornerId: number | null;
  onCornerSelect: (id: number | null) => void;
  selectedCornerDetail: CornerAnalysis | null;
}

export default function RightPanel({
  view, corners, selectedCornerId, onCornerSelect, selectedCornerDetail,
}: Props) {
  const opportunities = [...corners]
    .filter(c => c.delta > 0.05)
    .sort((a, b) => b.delta - a.delta)
    .slice(0, 3);

  const activeCoach = selectedCornerDetail ?? null;

  return (
    <div className="right-panel">

      {/* ── Sector times ── */}
      <div className="section">
        <div className="label" style={{ marginBottom: 6 }}>Sector Times</div>
        <div className="data-row"><span className="text-muted">sector data pending</span><span className="mono text-muted">—</span></div>
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
        <div className="data-row row--best" style={{ borderBottom: "none" }}>
          <span className="text-muted">Best lap</span>
          <span className="mono text-blue">{view ? view.bestLap : "—"}</span>
        </div>
      </div>

      {/* ── Top opportunities ── */}
      <div className="section section--hi">
        <div className="label" style={{ marginBottom: 8 }}>Top Opportunities</div>
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

        {/* Tyre */}
        <div className="section" style={{ flex: 1.5 }}>
          <div className="section__row" style={{ marginBottom: 8 }}>
            <span className="label">Tyre · temp</span>
            <div className="tyre-compound tyre-compound--na" title="compound unavailable">—</div>
          </div>
          <div style={{ display: "flex", justifyContent: "space-between" }}>
            {(["fl","fr","rl","rr"] as const).map(pos => {
              const t = view ? view.tyreTemps[pos] : { value: "—", tone: "none" as const };
              return (
                <div key={pos} className="tyre-col">
                  <span className="label" style={{ fontSize: 9 }}>{pos.toUpperCase()}</span>
                  <div className="tyre-bar"><div className="tyre-fill tyre-fill--na" /></div>
                  <span className="mono" style={{ fontSize: 9 }}>—</span>
                  <span className={`mono tyre-${t.tone}`} style={{ fontSize: 9 }}>{t.value}</span>
                </div>
              );
            })}
          </div>
        </div>

        {/* Fuel */}
        <div className="section" style={{ flex: 1 }}>
          <div className="label" style={{ marginBottom: 5 }}>Fuel</div>
          <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", marginBottom: 4 }}>
            <span className={`mono fuel-${view ? view.fuelTone : "none"}`} style={{ fontSize: 15, fontWeight: 600 }}>
              {view ? view.fuelPct : "—"}
            </span>
            <span className="label">map —</span>
          </div>
          <div className="data-row"><span className="text-muted">Used/lap</span><span className="mono text-muted">—</span></div>
          <div className="data-row" style={{ borderBottom: "none" }}><span className="text-muted">Laps left</span><span className="mono text-muted">—</span></div>
        </div>

      </div>
    </div>
  );
}
