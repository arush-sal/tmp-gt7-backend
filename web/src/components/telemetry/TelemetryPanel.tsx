import { useState, useCallback, useRef, useMemo } from "react";
import type { HistoryStore } from "../../lib/historyStore";
import type { LapView } from "../../lib/lapView";
import type { MedianDTO, ReferenceDTO } from "../../contract/session";
import { overlayColumns, type Overlay } from "../../lib/lapOverlays";
import { getCard, type CardDef } from "./cardCatalog";
import { loadCardConfig, saveCardConfig, defaultCardConfig, newCardState, TELEMETRY_CARDS_KEY, type PanelConfig, type CardState } from "../../lib/panelConfig";
import TelemetryGraph, { type HoverReadout } from "./TelemetryGraph";
import GraphLegend from "./GraphLegend";
import GraphPicker from "./GraphPicker";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  sortableKeyboardCoordinates,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

interface Props {
  store: HistoryStore; storageKey?: string;
  analysisLap?: LapView | null;
  analysisScrubIndex?: number | null;
  median?: MedianDTO | null;
  reference?: ReferenceDTO | null;
  // Shared Grafana-style zoom window (lap-time seconds); owned by Dashboard so the track canvas can
  // mirror it. null = full lap.
  xRange?: [number, number] | null;
  onXRange?: (r: [number, number] | null) => void;
}

/** Moves `activeKey` to `overKey`'s index; no-op on unknown/equal keys. */
export function reorder(order: string[], activeKey: string, overKey: string): string[] {
  const from = order.indexOf(activeKey), to = order.indexOf(overKey);
  if (from === -1 || to === -1 || from === to) return order;
  const next = [...order];
  next.splice(to, 0, next.splice(from, 1)[0]);
  return next;
}

function TelemetryCard({
  card, store, state, onToggleCollapse, onToggleSeries, onRemove,
  analysisLap, analysisScrubIndex, overlay, xRange, onXRange,
}: {
  card: CardDef; store: HistoryStore; state: CardState;
  onToggleCollapse: () => void; onToggleSeries: (k: string) => void; onRemove: () => void;
  analysisLap?: LapView | null; analysisScrubIndex?: number | null; overlay: Overlay;
  xRange: [number, number] | null; onXRange: (r: [number, number] | null) => void;
}) {
  const isMulti = card.kind === "multi";
  const collapsed = state.collapsed;
  const [hover, setHover] = useState<HoverReadout | null>(null); // cursor readout, shown in the legend
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id: card.key });
  const style = { transform: CSS.Transform.toString(transform), transition };
  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`telemetry-graph accordion-item ${!collapsed ? "telemetry-graph--expanded" : ""}`}
    >
      <div className="telemetry-graph__tabrow">
        <button
          type="button"
          className="telemetry-graph__drag"
          aria-label={`drag ${card.title}`}
          {...attributes}
          {...listeners}
        >
          ⠿
        </button>
        <button
          type="button"
          className="telemetry-graph__tab accordion-tab"
          onClick={onToggleCollapse}
          aria-expanded={!collapsed}
          aria-controls={`tg-${card.key}`}
        >
          <span>{collapsed ? "▸" : "▾"} <span className="telemetry-graph__title">{card.title}</span></span>
          {card.unit && <span className="telemetry-graph__unit-tag">{card.unit}</span>}
        </button>
        <button type="button" className="telemetry-graph__remove" aria-label={`remove ${card.title}`} onClick={onRemove}>×</button>
      </div>
      {!collapsed && (
        <>
          <TelemetryGraph
            // Re-key ONLY the graph (not the whole card) on overlay presence: TelemetryGraph fixes
            // its series count at mount, so uPlot must remount when median/reference availability
            // flips — but the card keeps its collapse/DnD/focus state.
            key={`${card.key}:${overlay.median != null}:${overlay.reference != null}`}
            store={store}
            channels={card.channels}
            label={card.title}
            unit={card.unit}
            id={`tg-${card.key}`}
            hidden={state.hidden}
            analysisLap={analysisLap}
            analysisScrubIndex={analysisScrubIndex}
            medianCol={overlay.median}
            referenceCol={overlay.reference}
            xRange={xRange}
            onXRange={onXRange}
            onHover={setHover}
          />
          <GraphLegend
            hover={hover}
            variant={isMulti ? "multi" : "single"}
            channels={card.channels}
            hidden={state.hidden}
            medianAvailable={overlay.median != null}
            referenceAvailable={overlay.reference != null}
            onToggle={onToggleSeries}
          />
        </>
      )}
    </div>
  );
}

/**
 * Hosts the live telemetry graphs as single-metric (or, for the tyre card, multi-series)
 * accordion-style cards, driven by the card catalog. State is a persisted `PanelConfig`
 * (order + per-card collapsed/hidden), seeded from localStorage and written back on every
 * mutation (add/remove/collapse/legend-toggle) so the layout survives a refresh. Always
 * floats (via the caller's FloatingPanel) over the center track-map canvas.
 */
export default function TelemetryPanel({
  store, storageKey = TELEMETRY_CARDS_KEY, analysisLap = null, analysisScrubIndex = null,
  median = null, reference = null, xRange = null, onXRange,
}: Props) {
  const [cfg, setCfg] = useState<PanelConfig>(() => loadCardConfig(storageKey, defaultCardConfig()));
  // Mutators read the latest cfg via this ref (not the render closure) so a same-tick
  // follow-up mutation never operates on a stale snapshot; render still uses `cfg` so
  // React re-renders. cfgRef.current === cfg during render, so cardState() is safe there too.
  const cfgRef = useRef(cfg);
  cfgRef.current = cfg;
  const [picking, setPicking] = useState(false);
  const applyXRange = onXRange ?? (() => {});

  const commit = useCallback((next: PanelConfig) => {
    cfgRef.current = next;          // keep the ref current so a same-tick follow-up mutation sees it
    setCfg(next);
    saveCardConfig(storageKey, next);
  }, [storageKey]);

  const cardState = (key: string): CardState => cfgRef.current.cards[key] ?? newCardState();

  const add = (key: string) => {
    setPicking(false);
    const cur = cfgRef.current;
    if (cur.order.includes(key)) return;
    commit({ order: [...cur.order, key], cards: { ...cur.cards, [key]: newCardState() } });
  };
  const remove = (key: string) => {
    const cur = cfgRef.current;
    const cards = { ...cur.cards }; delete cards[key];
    commit({ order: cur.order.filter(k => k !== key), cards });
  };
  const toggleCollapse = (key: string) => {
    const cur = cfgRef.current; const s = cardState(key);
    commit({ ...cur, cards: { ...cur.cards, [key]: { ...s, collapsed: !s.collapsed } } });
  };
  const toggleSeries = (key: string, seriesKey: string) => {
    if (seriesKey === "actual") return; // "current" is compulsory, never hideable
    const cur = cfgRef.current; const s = cardState(key);
    const hidden = s.hidden.includes(seriesKey) ? s.hidden.filter(x => x !== seriesKey) : [...s.hidden, seriesKey];
    commit({ ...cur, cards: { ...cur.cards, [key]: { ...s, hidden } } });
  };

  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e;
    if (!over || active.id === over.id) return;
    const cur = cfgRef.current;
    commit({ ...cur, order: reorder(cur.order, String(active.id), String(over.id)) });
  };

  const cards = cfg.order.map(getCard).filter((c): c is CardDef => !!c);

  // Median/reference overlay columns, resampled onto the selected lap per single-series card
  // (tyre/multi gets none). Memoized so the O(n) resample isn't redone on unrelated re-renders.
  const overlays = useMemo(() => {
    const map: Record<string, Overlay> = {};
    for (const card of cards) {
      map[card.key] = card.kind === "single"
        ? overlayColumns(analysisLap, card.channels[0], median, reference)
        : { median: null, reference: null };
    }
    return map;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cfg.order, analysisLap, median, reference]);

  return (
    <div className="telemetry-panel">
      <div className="telemetry-panel__header">
        <span className="heading">Telemetry</span>
      </div>
      <div className="telemetry-panel__list">
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext items={cfg.order} strategy={verticalListSortingStrategy}>
            {cards.map(card => {
              const overlay = overlays[card.key] ?? { median: null, reference: null };
              return (
                <TelemetryCard
                  key={card.key}
                  card={card}
                  store={store}
                  state={cardState(card.key)}
                  onToggleCollapse={() => toggleCollapse(card.key)}
                  onToggleSeries={sk => toggleSeries(card.key, sk)}
                  onRemove={() => remove(card.key)}
                  analysisLap={analysisLap}
                  analysisScrubIndex={analysisScrubIndex}
                  overlay={overlay}
                  xRange={xRange}
                  onXRange={applyXRange}
                />
              );
            })}
          </SortableContext>
        </DndContext>
      </div>
      <div className="telemetry-panel__footer">
        <button type="button" className="telemetry-panel__add" onClick={() => setPicking(true)}>+ Add graph</button>
      </div>
      {picking && (
        <GraphPicker shownKeys={cfg.order} onAdd={add} onClose={() => setPicking(false)} />
      )}
    </div>
  );
}
