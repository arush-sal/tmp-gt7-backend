import type { Corner, CornerAnalysis } from "../mock/snapshot";

interface Props {
  corners: Corner[];
  selectedCornerId: number | null;
  onCornerSelect: (id: number | null) => void;
  selectedCornerDetail: CornerAnalysis | null;
}

/** Maps a corner delta (seconds) to a display colour. */
function cornerFill(delta: number): string {
  if (delta >  0.08) return "#e10600"; // red   — lost time
  if (delta < -0.02) return "#2ed16a"; // green — gained
  return "#f5d020";                     // amber — neutral
}

function cornerTextFill(delta: number): string {
  return delta < -0.02 ? "#14361f" : delta <= 0.08 ? "#15171c" : "#fff";
}

// ---------------------------------------------------------------------------
// Full circuit view (default state)
// ---------------------------------------------------------------------------
function CircuitView({ corners, selectedCornerId, onCornerSelect }: {
  corners: Corner[];
  selectedCornerId: number | null;
  onCornerSelect: (id: number | null) => void;
}) {
  return (
    <div className="track-map card">
      <div className="track-map__header">
        <span className="heading">Track Map</span>
        <div className="chip-row">
          <span className="chip">racing line</span>
          <span className="chip">brake</span>
          <span className="chip">apex</span>
          <span className="chip">entry/exit</span>
          <span className="chip">delta heat</span>
          <span className="chip chip--dashed">⊕ zoom / pan</span>
        </div>
      </div>

      <div className="track-map__canvas">
        <div className="track-map__svg-wrap">
          <svg viewBox="0 0 600 460" style={{ width: "100%", height: "auto", display: "block" }}>
            {/* Track surface */}
            <path
              d="M315,412 L425,414 C480,416 502,392 496,348 C489,288 494,200 482,160 C472,126 448,104 410,112 C368,121 346,158 318,190 C306,202 300,210 282,212 C232,216 182,210 152,222 C120,235 96,250 86,282 C72,320 76,358 104,382 C126,401 148,408 175,408 L315,412 Z"
              fill="none" stroke="var(--track, #c4c4ce)" strokeWidth="19"
              strokeLinejoin="round" strokeLinecap="round"
            />
            {/* Track centre line */}
            <path
              d="M315,412 L425,414 C480,416 502,392 496,348 C489,288 494,200 482,160 C472,126 448,104 410,112 C368,121 346,158 318,190 C306,202 300,210 282,212 C232,216 182,210 152,222 C120,235 96,250 86,282 C72,320 76,358 104,382 C126,401 148,408 175,408 L315,412 Z"
              fill="none" stroke="var(--track-in, #eeeff4)" strokeWidth="12"
              strokeLinejoin="round" strokeLinecap="round"
            />
            {/* Start line */}
            <line x1="315" y1="401" x2="315" y2="423" stroke="var(--start-line, #3a3d44)" strokeWidth="2.5" />
            <path d="M342,412 h22 m-7,-6 l7,6 -7,6"
              fill="none" stroke="var(--start-line, #3a3d44)" strokeWidth="2"
              strokeLinecap="round" strokeLinejoin="round"
            />
            {/* Sector labels */}
            <text x="546" y="248" textAnchor="end"
              fontFamily="Space Grotesk, sans-serif" fontWeight="700" fontSize="26" fill="var(--sector-lbl, #d0d0da)">S1</text>
            <text x="30" y="318"
              fontFamily="Space Grotesk, sans-serif" fontWeight="700" fontSize="26" fill="var(--sector-lbl, #d0d0da)">S2</text>

            {/* Corner markers */}
            {corners.map((c) => {
              const isSelected = c.id === selectedCornerId;
              const fill = cornerFill(c.delta);
              const textFill = cornerTextFill(c.delta);
              const r = isSelected ? 16 : 13;
              return (
                <g key={c.id}
                  onClick={() => onCornerSelect(c.id === selectedCornerId ? null : c.id)}
                  style={{ cursor: "pointer" }}
                >
                  <circle cx={c.cx} cy={c.cy} r={r + 4} fill="transparent" />
                  <circle
                    cx={c.cx} cy={c.cy} r={r}
                    fill={fill}
                    stroke={isSelected ? "var(--text, #15171c)" : fill}
                    strokeWidth={isSelected ? 2.5 : 1.5}
                    opacity={isSelected ? 1 : 0.88}
                  />
                  <text
                    x={c.cx} y={c.cy + 4}
                    textAnchor="middle"
                    fontFamily="Spline Sans Mono, monospace"
                    fontSize="11" fontWeight="700"
                    fill={textFill}
                    style={{ pointerEvents: "none" }}
                  >
                    {c.id}
                  </text>
                </g>
              );
            })}
          </svg>
          <div className="track-map__hint">click any corner to inspect →</div>
        </div>
      </div>

      <div className="track-map__legend">
        <span className="label">Delta heatmap</span>
        <span className="chip chip--green">gained time</span>
        <span className="chip chip--amber">neutral</span>
        <span className="chip chip--red">lost time</span>
        <span style={{ flex: 1 }} />
        <span className="label">{corners.length} corners · S1/S2 · 386m</span>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Corner zoom view (corner selected state)
// ---------------------------------------------------------------------------
function CornerZoomView({ corner, onZoomOut }: {
  corner: CornerAnalysis;
  onZoomOut: () => void;
}) {
  const brakeDeltaM = corner.actualBrakePoint - corner.refBrakePoint;

  return (
    <div className="track-map card">
      <div className="track-map__header">
        <span className="heading">
          Track Map{" "}
          <span style={{ color: "var(--red)", fontWeight: 500, fontSize: 14 }}>
            — zoomed to {corner.name}
          </span>
        </span>
        <div className="chip-row">
          <span className="chip">brake</span>
          <span className="chip">turn-in</span>
          <span className="chip">apex</span>
          <span className="chip">exit</span>
          <button
            className="chip chip--dashed"
            onClick={onZoomOut}
            style={{ cursor: "pointer", background: "none", border: "none", font: "inherit" }}
          >
            ⊖ zoom out
          </button>
        </div>
      </div>

      <div className="track-map__canvas">
        <div className="track-map__svg-wrap" style={{ position: "relative" }}>
          <svg viewBox="0 0 600 460" style={{ width: "100%", height: "auto", display: "block" }}>
            {/* Corner path */}
            <path
              d="M488,58 C338,64 216,94 162,182 C112,260 112,332 174,400 C236,464 364,458 490,432"
              fill="none" stroke="var(--track, #c4c4ce)" strokeWidth="26"
              strokeLinejoin="round" strokeLinecap="round"
            />
            <path
              d="M488,58 C338,64 216,94 162,182 C112,260 112,332 174,400 C236,464 364,458 490,432"
              fill="none" stroke="var(--track-in, #eeeff4)" strokeWidth="16"
              strokeLinejoin="round" strokeLinecap="round"
            />
            <text x="52" y="250"
              fontFamily="Space Grotesk, sans-serif" fontWeight="700" fontSize="28" fill="var(--sector-lbl, #d0d0da)">S2</text>

            {/* B = brake, A = apex, X = exit */}
            {/* Actual brake point (red — earlier = farther right on entry) */}
            <g><circle cx="404" cy="62" r="14" fill="#e10600" opacity="0.88" />
              <text x="404" y="67" textAnchor="middle"
                fontFamily="Spline Sans Mono,monospace" fontSize="11" fontWeight="700" fill="#fff">B</text></g>
            {/* Reference brake point (green — later = farther left) */}
            <g><circle cx="320" cy="72" r="14" fill="#2ed16a" opacity="0.88" />
              <text x="320" y="77" textAnchor="middle"
                fontFamily="Spline Sans Mono,monospace" fontSize="11" fontWeight="700" fill="#14361f">B</text></g>
            {/* Reference apex (green) */}
            <g><circle cx="120" cy="278" r="14" fill="#2ed16a" opacity="0.88" />
              <text x="120" y="283" textAnchor="middle"
                fontFamily="Spline Sans Mono,monospace" fontSize="11" fontWeight="700" fill="#14361f">A</text></g>
            {/* Actual apex (red — late/inside) */}
            <g><circle cx="140" cy="338" r="14" fill="#e10600" opacity="0.88" />
              <text x="140" y="343" textAnchor="middle"
                fontFamily="Spline Sans Mono,monospace" fontSize="11" fontWeight="700" fill="#fff">A</text></g>
            {/* Exit (amber) */}
            <g><circle cx="306" cy="452" r="14" fill="#f5d020" opacity="0.88" />
              <text x="306" y="457" textAnchor="middle"
                fontFamily="Spline Sans Mono,monospace" fontSize="11" fontWeight="700" fill="#15171c">X</text></g>
          </svg>

          {/* Floating callouts */}
          <div className="map-callout map-callout--red" style={{ left: "63%", top: "2%" }}>
            brake {brakeDeltaM}m early
          </div>
          <div className="map-callout map-callout--red" style={{ left: "14%", top: "56%" }}>
            apex {corner.apexSpeedDelta} km/h
          </div>
          <div className="map-callout map-callout--amber" style={{ left: "55%", top: "90%" }}>
            exit +{corner.exitSpeedDelta} km/h
          </div>
        </div>
      </div>

      <div className="track-map__legend">
        <span className="label">B brake · A apex · X exit</span>
        <span style={{ flex: 1 }} />
        <span className="chip chip--red">actual (solid)</span>
        <span className="chip chip--green">reference (dashed)</span>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Exported component — switches between the two views
// ---------------------------------------------------------------------------
export default function TrackMap({ corners, selectedCornerId, onCornerSelect, selectedCornerDetail }: Props) {
  if (selectedCornerId !== null && selectedCornerDetail) {
    return (
      <CornerZoomView
        corner={selectedCornerDetail}
        onZoomOut={() => onCornerSelect(null)}
      />
    );
  }
  return (
    <CircuitView
      corners={corners}
      selectedCornerId={selectedCornerId}
      onCornerSelect={onCornerSelect}
    />
  );
}
