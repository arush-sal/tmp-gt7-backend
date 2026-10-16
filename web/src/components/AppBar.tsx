import StatusDot from "./StatusDot";

interface Props {
  theme: "light" | "dark";
  onToggleTheme: () => void;
  live: boolean;
}

/**
 * App chrome (not part of the wireframe spec) — holds the theme toggle so both
 * light and dark can be shown. Corner selection is driven by clicking the map
 * or a Top Opportunity row, per the wireframe interaction flow.
 */
export default function AppBar({ theme, onToggleTheme, live }: Props) {
  return (
    <div className="appbar">
      <span className="appbar__brand"><b>Garage</b> · Dashboard</span>
      <div className="appbar__controls">
        <StatusDot live={live} />
        <button className="toggle" onClick={onToggleTheme}>
          ◐ {theme === "dark" ? "Light" : "Dark"}
        </button>
      </div>
    </div>
  );
}
