import { useState } from "react";

const TABS = [
  "Braking Analysis",
  "Throttle Analysis",
  "Speed Analysis",
  "Coasting Analysis",
  "Steering Analysis",
  "Gear Analysis",
  "Tyre Analysis",
  "Fuel Analysis",
] as const;

export default function TelemetryAccordion() {
  const [open, setOpen] = useState<string>("Throttle Analysis");

  const toggle = (tab: string) => setOpen(prev => prev === tab ? "" : tab);

  return (
    <div className="telemetry card">
      <div className="telemetry__header">
        <span className="heading">Telemetry</span>
      </div>

      <div className="telemetry__list">
        {TABS.map(tab => (
          <div key={tab} className="accordion-item">
            <button
              className={`accordion-tab ${open === tab ? "accordion-tab--open" : ""}`}
              onClick={() => toggle(tab)}
            >
              <span>{open === tab ? "▾" : "▸"} {tab}</span>
            </button>

            {open === tab && (
              <div className="accordion-body">
                <div className="graph-ph">
                  <span className="label">{tab.replace(" Analysis", "").toLowerCase()} trace</span>
                  <span className="graph-ph__sub">ref · current · median — by distance</span>
                </div>
                <div className="graph-legend">
                  <span className="chip chip--green" style={{ fontSize: 10 }}>— reference</span>
                  <span className="chip" style={{ fontSize: 10 }}>— current</span>
                  <span className="chip chip--dashed" style={{ fontSize: 10 }}>·· median</span>
                </div>
              </div>
            )}
          </div>
        ))}

        <button className="accordion-add">＋ Custom Graphs</button>
      </div>
    </div>
  );
}
