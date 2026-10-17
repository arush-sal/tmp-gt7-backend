import StatusDot from "./StatusDot";

interface Props {
  theme: "light" | "dark";
  onToggleTheme: () => void;
  live: boolean;
  /** Callback ref for the slot Dashboard portals the status/record chip into; omit when no dashboard is mounted (state setter => re-renders once mounted). */
  recordSlotRef?: (el: HTMLElement | null) => void;
}

/**
 * App chrome (not part of the wireframe spec) — holds the theme toggle so both
 * light and dark can be shown. Corner selection is driven by clicking the map
 * or a Top Opportunity row, per the wireframe interaction flow.
 */
export default function AppBar({ theme, onToggleTheme, live, recordSlotRef }: Props) {
  return (
    <div className="appbar">
      <span className="appbar__brand"><b>Garage</b> · Dashboard</span>
      <div className="appbar__controls">
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
