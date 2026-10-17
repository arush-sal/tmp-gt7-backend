import type { ReactNode } from "react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Stage, Layer, Line, Text, Shape, Circle, RegularPolygon } from "react-konva";
import type Konva from "konva";
import type { HistoryStore, TracePoint } from "../../lib/historyStore";
import {
  fitView, worldToScreen, screenToWorld, bboxOf, type View,
} from "../../lib/trackTransform";
import { nearestSampleIndex } from "../../lib/trackScrub";
import { peakDeltaColor } from "../../lib/traceColor";
import type { DrawGeometry } from "../../lib/rkbGeometry";
import { geometryBbox } from "../../lib/rkbGeometry";
import { drawTrace } from "./drawTrace";

const PAD = 24;
const MIN_EXTENT_M = 50;

// Canvas 2D cannot resolve CSS var() strings -- Konva shapes need literal colours.
// Values mirror src/index.css :root (light) and [data-theme="dark"] overrides.
const PALETTE = {
  light: {
    sectorLabel: "#c2c2cc",
    startLine: "#2a2d34",
  },
  dark: {
    sectorLabel: "#5a616a",
    startLine: "#c8cdd3",
  },
} as const;

// A speed peak/valley at a track world position. `index` is the ExtremumDTO.index (shared id with
// the peaks table for cross-highlight). Symbol colour is by KIND; deltaNs feeds the pinned label.
export type PeakMarker = { index: number; x: number; z: number; kind: "peak" | "valley"; deltaNs: number | null };

const PEAK_FILL = "#2ed16a";   // peak symbol green
const VALLEY_FILL = "#e10600"; // valley symbol red

// Signed delta seconds for the pinned canvas label, e.g. "+0.097s".
const fmtDeltaS = (ns: number | null): string =>
  ns == null ? "—" : `${ns >= 0 ? "+" : ""}${(ns / 1e9).toFixed(3)}s`;

export default function TrackTraceCanvas(props: {
  store: HistoryStore; geometry: DrawGeometry | null; theme: "light" | "dark";
  footer?: ReactNode;
  selectedLap?: "all" | "current" | number;
  analysisLap?: ReadonlyArray<TracePoint> | null;
  referenceLap?: ReadonlyArray<TracePoint> | null;
  overlayLaps?: ReadonlyArray<ReadonlyArray<TracePoint>>; // "All laps": every completed lap, faint
  // Shared graph zoom window (lap-time seconds); when set, the canvas frames just that track segment.
  zoomXRange?: [number, number] | null;
  // In analysis mode the zoom/reset controls drive the shared window (bidirectional with the graph).
  // anchorT (lap-time seconds) keeps that point fixed while zooming (cursor-anchored wheel zoom);
  // omitted -> zoom around the window centre (buttons).
  onZoomWindow?: (factor: number, anchorT?: number) => void; // >1 = zoom out (widen), <1 = zoom in (narrow)
  onResetView?: () => void;
  peakMarkers?: ReadonlyArray<PeakMarker>;
  hoveredPeak?: number | null;
  pinnedPeak?: number | null;
  onHoverPeak?: (index: number | null) => void;
  onPinPeak?: (index: number) => void;
  scrubIndex?: number | null;
  onScrub?: (i: number) => void;
  referenceControl?: ReactNode; // reference SOURCE switch, rendered beside the "reference on" toggle
}) {
  const {
    store, geometry, theme, footer, selectedLap = "all",
    analysisLap = null, referenceLap = null, overlayLaps = [], zoomXRange = null,
    onZoomWindow, onResetView, peakMarkers = [],
    hoveredPeak = null, pinnedPeak = null, onHoverPeak, onPinPeak,
    scrubIndex = null, onScrub, referenceControl = null,
  } = props;
  // Analysis mode: the zoom/reset controls manipulate the shared lap-time window (which also drives
  // the graphs), instead of the canvas's own spatial zoom used in live mode.
  const windowed = analysisLap != null;
  const [showReference, setShowReference] = useState(true);

  const containerRef = useRef<HTMLDivElement | null>(null);
  const stageRef = useRef<Konva.Stage | null>(null);
  const staticLayerRef = useRef<Konva.Layer | null>(null);
  const traceLayerRef = useRef<Konva.Layer | null>(null);
  const traceShapeRef = useRef<Konva.Shape | null>(null);

  const [size, setSize] = useState({ w: 1, h: 1 });
  const [zoom, setZoom] = useState(1);
  const [follow, setFollow] = useState(true);
  const [manualView, setManualView] = useState<View | null>(null);
  // Windowed free-pan: once the user drags while zoomed, the spatial manualView takes over from the
  // graph-window segment fit (no snap-back). Any window change / reset clears it and re-syncs.
  const [panned, setPanned] = useState(false);
  const lastRevision = useRef(-1);

  // --- Stage sizing: ResizeObserver on the container ---
  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(entries => {
      for (const e of entries) {
        const w = Math.max(1, Math.round(e.contentRect.width));
        const h = Math.max(1, Math.round(e.contentRect.height));
        setSize({ w, h });
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // --- Compute the view (auto-fit unless the user has taken manual control) ---
  const geomBbox = geometry ? geometryBbox(geometry) : null;
  const currentTrace = store.trace().current;
  const traceBbox = bboxOf(currentTrace);
  // Fit target: when geometry is loaded, fit to the RKB geometry bbox ALONE. The
  // driven trace lives in the same world frame, so it will sit within/around the
  // track; a stray pre-race spawn/grid->race teleport point simply falls off-canvas,
  // which is fine. Only fall back to the trace bbox when there is no geometry.
  // (Auto-zooming out to accommodate a trace that genuinely exceeds the track bbox
  // is a deferred refinement.)
  const fitBb = geomBbox ?? traceBbox;

  // Graph-linked segment zoom: when a telemetry-graph time window is active, frame just the track
  // samples inside that window (overrides the full-track fit + any manual pan/zoom).
  const segBbox = (zoomXRange && analysisLap)
    ? bboxOf(analysisLap.filter(p => p.t != null && p.t >= zoomXRange[0] && p.t <= zoomXRange[1]))
    : null;

  let view: View;
  if (segBbox && !panned) {
    view = fitView(segBbox, size.w, size.h, PAD, 1, MIN_EXTENT_M); // window governs (until a drag)
  } else if (panned && manualView) {
    view = manualView; // windowed free pan — no snap-back; cleared on the next window change/reset
  } else if (follow && fitBb) {
    view = fitView(fitBb, size.w, size.h, PAD, zoom, MIN_EXTENT_M);
  } else if (manualView) {
    view = manualView;
  } else if (fitBb) {
    view = fitView(fitBb, size.w, size.h, PAD, 1, MIN_EXTENT_M);
  } else {
    view = { scale: 1, offsetX: 0, offsetZ: 0 };
  }

  // A window change (graph-select, canvas zoom, scrub-page) or a new lap re-couples the canvas:
  // drop the free-pan override so it snaps back to the shared segment.
  useEffect(() => { setPanned(false); setManualView(null); }, [zoomXRange]);

  // --- Static geometry layer: drawn UNCACHED. Caching baked the view transform into an
  // offscreen bitmap sized to the content's screen bbox; when zoomed in, that bbox spans
  // a huge pixel area -> Konva allocates a gigantic canvas -> the tab freezes/OOMs. The
  // layer holds only sector labels and the start/finish tick (no track surface is drawn:
  // a mis-detected track must not paint a wrong layout), so caching buys nothing here.
  // react-konva updates the Line/Text nodes from `view` on re-render; just nudge a redraw on view/geometry/theme change.
  useEffect(() => {
    staticLayerRef.current?.batchDraw();
  }, [geometry, theme, size.w, size.h, view.scale, view.offsetX, view.offsetZ]);

  // --- rAF loop: redraw the trace layer only when the store's revision changes ---
  useEffect(() => {
    let raf = 0;
    const tick = () => {
      const rev = store.revision();
      if (rev !== lastRevision.current) {
        lastRevision.current = rev;
        traceLayerRef.current?.batchDraw();
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [store]);

  // redraw trace layer immediately on view/theme/selectedLap/reference change (independent of rAF)
  useEffect(() => {
    traceLayerRef.current?.batchDraw();
  }, [view.scale, view.offsetX, view.offsetZ, theme, selectedLap, referenceLap, showReference, overlayLaps]);

  // Shared center-anchored zoom math: given the current view/zoom and a factor, compute
  // the clamped new zoom plus the manualView that keeps `pointer` (screen coords, relative
  // to the stage) fixed on screen. Used by both the wheel handler (pointer = cursor) and
  // the zoom buttons (pointer = stage center).
  function zoomAt(pointer: { x: number; y: number }, factor: number) {
    const oldScale = view.scale;
    const newZoom = Math.max(1, Math.min(20, zoom * factor));
    // keep the world point under `pointer` fixed on screen
    const worldX = pointer.x / oldScale + view.offsetX;
    const worldZ = pointer.y / oldScale + view.offsetZ;
    // derive newScale from the CLAMPED zoom (not the raw factor), so scale stops
    // changing once zoom saturates at [1, 20] instead of drifting past it.
    const newScale = oldScale * (newZoom / zoom);
    const newOffsetX = worldX - pointer.x / newScale;
    const newOffsetZ = worldZ - pointer.y / newScale;
    return { zoom: newZoom, view: { scale: newScale, offsetX: newOffsetX, offsetZ: newOffsetZ } };
  }

  function handleWheel(e: Konva.KonvaEventObject<WheelEvent>) {
    e.evt.preventDefault();
    const stage = stageRef.current;
    if (!stage) return;
    const pointer = stage.getPointerPosition();
    if (windowed) {
      // Anchor the window zoom on the track point under the cursor (its lap-time), so zoom-in
      // converges toward the cursor instead of the window centre. scroll up = zoom in.
      let anchorT: number | undefined;
      if (pointer && analysisLap) {
        const { x, z } = screenToWorld(view, pointer.x, pointer.y);
        const idx = nearestSampleIndex(analysisLap, x, z);
        if (idx >= 0) anchorT = analysisLap[idx].t;
      }
      onZoomWindow?.(e.evt.deltaY > 0 ? 1.25 : 1 / 1.25, anchorT);
      return;
    }
    const dir = e.evt.deltaY > 0 ? -1 : 1;
    const factor = dir > 0 ? 1.1 : 1 / 1.1;

    if (pointer) {
      const next = zoomAt(pointer, factor);
      setManualView(next.view);
      setZoom(next.zoom);
    } else {
      setZoom(Math.max(1, Math.min(20, zoom * factor)));
    }
    setFollow(false);
  }

  function handleZoomButton(factor: number) {
    const center = { x: size.w / 2, y: size.h / 2 };
    const next = zoomAt(center, factor);
    setManualView(next.view);
    setZoom(next.zoom);
    setFollow(false);
  }

  function handleZoomIn() { if (windowed) { onZoomWindow?.(1 / 1.25); return; } handleZoomButton(1.1); }
  function handleZoomOut() { if (windowed) { onZoomWindow?.(1.25); return; } handleZoomButton(1 / 1.1); }

  function handleDragMove(e: Konva.KonvaEventObject<DragEvent>) {
    const stage = e.target;
    const dxScreen = stage.x();
    const dzScreen = stage.y();
    // convert screen-space drag delta into world-space offset delta
    const newView: View = {
      scale: view.scale,
      offsetX: view.offsetX - dxScreen / view.scale,
      offsetZ: view.offsetZ - dzScreen / view.scale,
    };
    // reset the stage's own transform (we bake the pan into `view` instead)
    stage.position({ x: 0, y: 0 });
    setManualView(newView);
    setFollow(false);
    if (windowed) setPanned(true); // decouple from the segment fit -> free pan, no snap-back
  }

  function handleFollowToggle() {
    setFollow(true);
    setManualView(null);
    setZoom(1);
    setPanned(false);
    if (windowed) onResetView?.(); // absolute reset: full lap window -> full track + full graphs
  }

  function handleStageClick() {
    if (!analysisLap || !onScrub) return;
    const stage = stageRef.current;
    if (!stage) return;
    const pointer = stage.getPointerPosition();
    if (!pointer) return;
    const { x, z } = screenToWorld(view, pointer.x, pointer.y);
    const idx = nearestSampleIndex(analysisLap, x, z);
    if (idx >= 0) onScrub(idx);
  }

  function handleExportPng() {
    const stage = stageRef.current;
    if (!stage) return;
    const dataUrl = stage.toDataURL({ pixelRatio: window.devicePixelRatio || 1 });
    const a = document.createElement("a");
    a.href = dataUrl;
    a.download = "track-trace.png";
    a.click();
  }

  const dpr = typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1;

  return (
    <div className="track-trace-canvas card">
      <div className="track-trace-canvas__header">
        <span className="heading">Track Trace</span>
        <div className="chip-row">
          <button type="button" className="chip" onClick={handleFollowToggle} aria-pressed={follow}>
            {follow ? "following" : "follow"}
          </button>
          <button type="button" className="chip" onClick={handleZoomOut} aria-label="zoom out">-</button>
          <button type="button" className="chip" onClick={handleZoomIn} aria-label="zoom in">+</button>
          <button type="button" className="chip" onClick={handleFollowToggle} aria-label="reset view">reset</button>
          {referenceLap && referenceLap.length > 1 && (
            <button
              type="button"
              className="chip"
              onClick={() => setShowReference(v => !v)}
              aria-pressed={showReference}
              title="toggle the reference line"
            >
              {showReference ? "reference on" : "reference off"}
            </button>
          )}
          {referenceControl}
          <button type="button" className="chip" onClick={handleExportPng}>export png</button>
        </div>
      </div>
      <div ref={containerRef} className="track-trace-canvas__stage-wrap">
        <Stage
          ref={stageRef}
          width={size.w}
          height={size.h}
          pixelRatio={dpr}
          draggable
          onWheel={handleWheel}
          onDragEnd={handleDragMove}
          onClick={handleStageClick}
        >
          <Layer ref={staticLayerRef} listening={false}>
            {geometry?.sectors.map(s => {
              const { sx, sy } = worldToScreen(view, s.x, s.z);
              return <Text key={s.label} x={sx} y={sy} text={s.label} fontSize={12} fill={PALETTE[theme].sectorLabel} />;
            })}
            {geometry?.startFinish && (() => {
              const { sx, sy } = worldToScreen(view, geometry.startFinish[0], geometry.startFinish[1]);
              return <Line points={[sx, sy - 10, sx, sy + 10]} stroke={PALETTE[theme].startLine} strokeWidth={2.5} />;
            })()}
          </Layer>
          <Layer ref={traceLayerRef} listening={false}>
            <Shape
              ref={traceShapeRef}
              sceneFunc={(ctx) => {
                drawTrace(ctx._context, {
                  trace: store.trace(), view, theme, select: selectedLap,
                  analysisLap: analysisLap ?? undefined,
                  referenceLap: showReference ? referenceLap : null,
                  overlayLaps,
                });
              }}
            />
            {analysisLap && scrubIndex !== null && scrubIndex >= 0 && scrubIndex < analysisLap.length && (() => {
              const p = analysisLap[scrubIndex];
              if (!Number.isFinite(p.posX) || !Number.isFinite(p.posZ)) return null;
              const { sx, sy } = worldToScreen(view, p.posX, p.posZ);
              return (
                <Circle x={sx} y={sy} radius={6} fill="#ffffff" stroke="#1a1a1a" strokeWidth={2} />
              );
            })()}
          </Layer>
          {/* Interactive peak/valley markers on their own listening layer (the trace layer is inert).
              Symbol green (peak) / red (valley); the hovered or pinned one is enlarged; the pinned one
              also shows its delta value. Hover/click cross-highlight with the peaks table. */}
          <Layer>
            {peakMarkers.map((m) => {
              if (!Number.isFinite(m.x) || !Number.isFinite(m.z)) return null;
              const { sx, sy } = worldToScreen(view, m.x, m.z);
              const up = m.kind === "peak";
              const active = m.index === hoveredPeak || m.index === pinnedPeak;
              const pinned = m.index === pinnedPeak;
              return (
                <RegularPolygon
                  key={m.index}
                  x={sx}
                  y={sy + (up ? -9 : 9)}
                  sides={3}
                  radius={active ? 8 : 5}
                  rotation={up ? 0 : 180}
                  fill={up ? PEAK_FILL : VALLEY_FILL}
                  stroke={active ? "#ffffff" : "#1a1a1a"}
                  strokeWidth={active ? 2 : 1}
                  shadowColor="#000"
                  shadowBlur={pinned ? 6 : 0}
                  onMouseEnter={() => onHoverPeak?.(m.index)}
                  onMouseLeave={() => onHoverPeak?.(null)}
                  onClick={(e) => { e.cancelBubble = true; onPinPeak?.(m.index); }}
                />
              );
            })}
            {pinnedPeak !== null && (() => {
              const m = peakMarkers.find(pm => pm.index === pinnedPeak);
              if (!m || !Number.isFinite(m.x) || !Number.isFinite(m.z)) return null;
              const { sx, sy } = worldToScreen(view, m.x, m.z);
              return (
                <Text
                  x={sx + 8}
                  y={sy - (m.kind === "peak" ? 22 : 4)}
                  text={fmtDeltaS(m.deltaNs)}
                  fontSize={13}
                  fontStyle="bold"
                  fill={peakDeltaColor(m.deltaNs)}
                  listening={false}
                />
              );
            })()}
          </Layer>
        </Stage>
        <div className="trace-legend" aria-label="drive line colours">
          <span className="trace-legend__item"><i className="trace-legend__sw trace-legend__sw--accel" />Throttle</span>
          <span className="trace-legend__item"><i className="trace-legend__sw trace-legend__sw--coast" />Coast</span>
          <span className="trace-legend__item"><i className="trace-legend__sw trace-legend__sw--brake" />Brake</span>
          <span className="trace-legend__item"><i className="trace-legend__sw trace-legend__sw--trail" />Trail</span>
          {referenceLap && referenceLap.length > 1 && showReference && (
            <span className="trace-legend__item"><i className="trace-legend__sw" style={{ background: "#F433FF" }} />Reference</span>
          )}
        </div>
      </div>
      {footer && <div className="track-status" role="status">{footer}</div>}
    </div>
  );
}
