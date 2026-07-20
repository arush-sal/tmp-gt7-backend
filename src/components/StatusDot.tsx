export default function StatusDot({ live }: { live: boolean }) {
  return <span className={`status-dot status-dot--${live ? "on" : "off"}`}
    title={live ? "live" : "not live"} aria-label={live ? "live" : "not live"} role="status" />;
}
