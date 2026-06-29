import type { DashboardSnapshot } from "../contract/dashboard";
import type { Corner, CornerAnalysis } from "../mock/snapshot";
import { formatLapTime, formatDelta, standingClass } from "../lib/format";

interface Props {
  snapshot: DashboardSnapshot;
  corners: Corner[];
  selectedCornerId: number | null;
  onCornerSelect: (id: number | null) => void;
  selectedCornerDetail: CornerAnalysis | null;
}

const COMPOUND_COLOR: Record<string, string> = {
  RS: "#e10600", RM: "#f5d020", RH: "#d0d0d8", IM: "#2ed16a", W: "#1e6fd9",
};
const COMPOUND_TEXT: Record<string, string> = {
  RS: "#fff", RM: "#15171c", RH: "#15171c", IM: "#14361f", W: "#fff",
};

function wearColor(wear: number): string {
  if (wear >= 85) return "#2ed16a";
  if (wear >= 60) return "#f5d020";
  return "#e10600";
}

export default function RightPanel({
  snapshot, corners, selectedCornerId, onCornerSelect, selectedCornerDetail,
}: Props) {
  const { sectors, lap, tyres, fuel, coach } = snapshot;

  const opportunities = [...corners]
    .filter(c => c.delta > 0.05)
    .sort((a, b) => b.delta - a.delta)
    .slice(0, 3);

  const hotTyre = tyres.find(t => t.temp > 90);
  const compound = tyres[0]?.compound ?? "RS";

  const activeCoach = selectedCornerDetail ?? null;

  return (
    <div className="right-panel">

      {/* ── Sector times ── */}
      <div className="section">
        <div className="label" style={{ marginBottom: 6 }}>Sector Times</div>
        {sectors.map(s => (
          <div key={s.number} className="data-row">
            <span className="body-text" style={{ width: 20 }}>S{s.number}</span>
            <span className={`mono ${standingClass(s.previousSectorStanding)}`}>
              {(s.finishTime / 1000).toFixed(3)}
            </span>
            <span className={s.liveDelta > 0 ? "pill-red" : "pill-green"}>
              {formatDelta(s.liveDelta)}
            </span>
          </div>
        ))}
      </div>

      {/* ── Lap history ── */}
      <div className="section">
        <div className="label" style={{ marginBottom: 6 }}>Lap History</div>
        <div className="data-row">
          <span className="text-muted">Current · L{lap.number}</span>
          <span className="mono text-muted">in progress</span>
        </div>
        {lap.previousLapTime != null && (
          <div className="data-row">
            <span className="text-muted">Previous</span>
            <span className={`mono ${standingClass(lap.previousLapStanding)}`}>
              {formatLapTime(lap.previousLapTime)}
            </span>
          </div>
        )}
        <div className="data-row" style={{ borderBottom: "none" }}>
          <span className="text-muted">Best lap</span>
          <span className={`mono ${standingClass(lap.bestLapStanding)}`}>
            {formatLapTime(lap.bestLapTime)}
          </span>
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
            {activeCoach ? activeCoach.coaching.confidence : "High"}
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
            <div className="coaching-title">
              Turn {coach.turn.count}{coach.turn.name ? ` ${coach.turn.name}` : ""}
            </div>
            <div className="label label--red" style={{ marginTop: 4, marginBottom: 2 }}>Problem</div>
            <p className="body-text">{coach.problem}</p>
            <div className="label label--green" style={{ marginTop: 4, marginBottom: 2 }}>Action</div>
            <p className="body-text">{coach.action}</p>
          </>
        )}

        <div className="data-row" style={{ borderBottom: "none", marginTop: 6 }}>
          <span className="text-muted">Expected gain</span>
          <span className="mono text-green" style={{ fontWeight: 700, fontSize: 14 }}>
            {(activeCoach ? activeCoach.coaching.expectedGain : coach.expectedGain).toFixed(2)}s
          </span>
        </div>
      </div>

      {/* ── Tyre + Fuel row ── */}
      <div style={{ display: "flex", gap: 8 }}>

        {/* Tyre */}
        <div className="section" style={{ flex: 1.5 }}>
          <div className="section__row" style={{ marginBottom: 8 }}>
            <span className="label">Tyre · wear &amp; temp</span>
            <div
              className="tyre-compound"
              style={{
                background: COMPOUND_COLOR[compound],
                color: COMPOUND_TEXT[compound],
              }}
              title={compound}
            >
              {compound}
            </div>
          </div>

          <div style={{ display: "flex", justifyContent: "space-between" }}>
            {tyres.map(t => (
              <div key={t.position} className="tyre-col">
                <span className="label" style={{ fontSize: 9 }}>{t.position}</span>
                <div className="tyre-bar">
                  <div
                    className="tyre-fill"
                    style={{ height: `${t.wear}%`, background: wearColor(t.wear) }}
                  />
                </div>
                <span className="mono" style={{ fontSize: 9, fontWeight: 600 }}>{t.wear}%</span>
                <span
                  className={`mono ${t.temp > 90 ? "text-red" : "text-muted"}`}
                  style={{ fontSize: 9 }}
                >
                  {t.temp}°
                </span>
              </div>
            ))}
          </div>

          {hotTyre && (
            <div className="data-row" style={{ borderBottom: "none", marginTop: 5, fontSize: 9 }}>
              <span className="text-red">⚠ {hotTyre.position} hot</span>
              <span className="mono text-red">{hotTyre.temp}°C</span>
            </div>
          )}
        </div>

        {/* Fuel */}
        <div className="section" style={{ flex: 1 }}>
          <div className="label" style={{ marginBottom: 5 }}>Fuel</div>
          <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", marginBottom: 4 }}>
            <span className="mono" style={{ fontSize: 15, fontWeight: 600 }}>{fuel.remaining}L</span>
            <span className="label">map {fuel.map}</span>
          </div>
          <div className="data-row">
            <span className="text-muted">Used/lap</span>
            <span className="mono">1.9L</span>
          </div>
          <div className="data-row" style={{ borderBottom: "none" }}>
            <span className="text-muted">Laps left</span>
            <span className="mono">{fuel.worthLap}</span>
          </div>
        </div>

      </div>
    </div>
  );
}
