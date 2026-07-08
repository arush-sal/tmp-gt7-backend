import { useState } from "react";
import type { HistoryStore } from "../../lib/historyStore";
import { getCard, DEFAULT_CARD_KEYS, type CardDef } from "./cardCatalog";
import TelemetryGraph from "./TelemetryGraph";
import GraphLegend from "./GraphLegend";

interface Props {
  store: HistoryStore;
}

function TelemetryCard({ card, store }: { card: CardDef; store: HistoryStore }) {
  const [collapsed, setCollapsed] = useState(false);
  const [hidden, setHidden] = useState<string[]>([]);
  const toggle = (k: string) => setHidden(h => (h.includes(k) ? h.filter(x => x !== k) : [...h, k]));
  const isMulti = card.kind === "multi";

  return (
    <div className={`telemetry-graph accordion-item ${!collapsed ? "telemetry-graph--expanded" : ""}`}>
      <button
        type="button"
        className="telemetry-graph__tab accordion-tab"
        onClick={() => setCollapsed(c => !c)}
        aria-expanded={!collapsed}
        aria-controls={`tg-${card.key}`}
      >
        <span>{collapsed ? "▸" : "▾"} {card.title}</span>
        {card.unit && <span className="telemetry-graph__unit-tag">{card.unit}</span>}
      </button>
      {!collapsed && (
        <>
          <TelemetryGraph
            key={card.key}
            store={store}
            channels={card.channels}
            label={card.title}
            unit={card.unit}
            id={`tg-${card.key}`}
            median={!isMulti}
            hidden={hidden}
          />
          <GraphLegend
            variant={isMulti ? "multi" : "single"}
            channels={card.channels}
            hidden={hidden}
            referenceAvailable={false}
            onToggle={toggle}
          />
        </>
      )}
    </div>
  );
}

/**
 * Hosts the live telemetry graphs as single-metric (or, for the tyre card, multi-series)
 * accordion-style cards, driven by the card catalog. Renders a FIXED set (DEFAULT_CARD_KEYS)
 * for now — add/remove/reorder + persistence land in a later task. Always floats (via the
 * caller's FloatingPanel) over the center track-map canvas.
 */
export default function TelemetryPanel({ store }: Props) {
  const cards = DEFAULT_CARD_KEYS.map(getCard).filter((c): c is CardDef => !!c);
  return (
    <div className="telemetry-panel">
      <div className="telemetry-panel__header">
        <span className="heading">Telemetry</span>
      </div>
      <div className="telemetry-panel__list">
        {cards.map(card => <TelemetryCard key={card.key} card={card} store={store} />)}
      </div>
    </div>
  );
}
