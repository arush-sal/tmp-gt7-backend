import StatusDot from "./StatusDot";
import type { AppMode } from "../hooks/useMode";

interface Props {
  theme: "light" | "dark";
  onToggleTheme: () => void;
  live: boolean;
  mode?: AppMode;   // "analysis" adds a badge; the dashboard portals Play/Pause into the slot instead of the live chip
  /** Callback ref for the slot Dashboard portals the status/record chip into; omit when no dashboard is mounted (state setter => re-renders once mounted). */
  recordSlotRef?: (el: HTMLElement | null) => void;
}

/**
 * App chrome (not part of the wireframe spec) — holds the theme toggle so both
 * light and dark can be shown. Corner selection is driven by clicking the map
 * or a Top Opportunity row, per the wireframe interaction flow.
 */
export default function AppBar({ theme, onToggleTheme, live, mode, recordSlotRef }: Props) {
  return (
    <div className="appbar">
      <span className="appbar__brand"><b>Garage</b> · Dashboard</span>
      <div className="appbar__controls">
        {mode === "analysis" && <span className="chip chip--amber" title="Stored-session analysis: no live telemetry">ANALYSIS</span>}
        {/* Hosted dashboard: it portals the merged status/record chip here (the only indicator).
            No dashboard (onboarding): plain connection dot. */}
        {recordSlotRef ? <span className="appbar__record" ref={recordSlotRef} /> : <StatusDot live={live} />}
        <button className="toggle" onClick={onToggleTheme}>
          ◐ {theme === "dark" ? "Light" : "Dark"}
        </button>
      </div>
    </div>
  );
}
