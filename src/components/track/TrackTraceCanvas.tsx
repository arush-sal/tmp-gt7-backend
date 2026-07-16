import type { ReactNode } from "react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Stage, Layer, Line, Text, Shape, Circle } from "react-konva";
import type Konva from "konva";
import type { HistoryStore } from "../../lib/historyStore";
import {
  fitView, worldToScreen, screenToWorld, bboxOf, type View,
} from "../../lib/trackTransform";
import { nearestSampleIndex } from "../../lib/trackScrub";
import type { DrawGeometry } from "../../lib/rkbGeometry";
import { geometryBbox } from "../../lib/rkbGeometry";
import { drawTrace } from "./drawTrace";

const PAD = 24;
const MIN_EXTENT_M = 50;

// Canvas 2D cannot resolve CSS var() strings -- Konva shapes need literal colours.
// Values mirror src/index.css :root (light) and [data-theme="dark"] overrides.
const PALETTE = {
  light: {
    trackBand: "rgba(120,122,134,0.65)",
    trackBandEdge: "#8d8d99",
    sectorLabel: "#d0d0da",
    startLine: "#3a3d44",
  },
  dark: {
    trackBand: "rgba(150,155,163,0.55)",
    trackBandEdge: "#5b6067",
    sectorLabel: "#474d54",
    startLine: "#c8cdd3",
  },
} as const;

function flatten(pts: ReadonlyArray<[number, number]>, view: View): number[] {
  const out: number[] = [];
  for (const [x, z] of pts) {
    const { sx, sy } = worldToScreen(view, x, z);
    out.push(sx, sy);
  }
  return out;
}

export default function TrackTraceCanvas(props: {
  store: HistoryStore; geometry: DrawGeometry | null; theme: "light" | "dark";
  footer?: ReactNode;
  selectedLap?: "all" | "current" | number;
  analysisLap?: ReadonlyArray<{ posX: number; posZ: number }> | null;
  scrubIndex?: number | null;
  onScrub?: (i: number) => void;
}) {
  const {
    store, geometry, theme, footer, selectedLap = "all",
    analysisLap = null, scrubIndex = null, onScrub,
  } = props;

  const containerRef = useRef<HTMLDivElement | null>(null);
  const stageRef = useRef<Konva.Stage | null>(null);
  const staticLayerRef = useRef<Konva.Layer | null>(null);
  const traceLayerRef = useRef<Konva.Layer | null>(null);
  const traceShapeRef = useRef<Konva.Shape | null>(null);

  const [size, setSize] = useState({ w: 1, h: 1 });
  const [zoom, setZoom] = useState(1);
  const [follow, setFollow] = useState(true);
  const [manualView, setManualView] = useState<View | null>(null);
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

  let view: View;
  if (follow && fitBb) {
    view = fitView(fitBb, size.w, size.h, PAD, zoom, MIN_EXTENT_M);
  } else if (manualView) {
    view = manualView;
  } else if (fitBb) {
    view = fitView(fitBb, size.w, size.h, PAD, 1, MIN_EXTENT_M);
  } else {
    view = { scale: 1, offsetX: 0, offsetZ: 0 };
  }

  // --- Static geometry layer: drawn UNCACHED. Caching baked the view transform into an
  // offscreen bitmap sized to the content's screen bbox; when zoomed in, that bbox spans
  // a huge pixel area -> Konva allocates a gigantic canvas -> the tab freezes/OOMs. The
  // band is a single Konva.Line (fast to draw natively each redraw), so caching buys
  // nothing here. react-konva updates the Line/Text nodes from `view` on re-render; just
  // nudge a redraw on view/geometry/theme change.
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

  // redraw trace layer immediately on view/theme/selectedLap change (independent of rAF polling)
  useEffect(() => {
    traceLayerRef.current?.batchDraw();
  }, [view.scale, view.offsetX, view.offsetZ, theme, selectedLap]);

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

  function handleZoomIn() { handleZoomButton(1.1); }
  function handleZoomOut() { handleZoomButton(1 / 1.1); }

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
  }

  function handleFollowToggle() {
    setFollow(true);
    setManualView(null);
    setZoom(1);
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
            {geometry && geometry.cl.length > 1 && geometry.widthM !== null && geometry.widthM > 0 && (
              <Line
                points={flatten(geometry.cl, view)}
                stroke={PALETTE[theme].trackBand}
                strokeWidth={Math.max(2, geometry.widthM * view.scale)}
                lineCap="round"
                lineJoin="round"
                listening={false}
              />
            )}
            {geometry && !(geometry.cl.length > 1 && geometry.widthM !== null && geometry.widthM > 0)
              && geometry.left.length > 1 && geometry.right.length > 1 && (
              <>
                <Line
                  points={[...flatten(geometry.left, view), ...flatten([...geometry.right].reverse(), view)]}
                  closed
                  fill={PALETTE[theme].trackBand}
                  stroke="transparent"
                />
                <Line points={flatten(geometry.left, view)} stroke={PALETTE[theme].trackBandEdge} strokeWidth={1.5} />
                <Line points={flatten(geometry.right, view)} stroke={PALETTE[theme].trackBandEdge} strokeWidth={1.5} />
              </>
            )}
            {geometry && !(geometry.cl.length > 1 && geometry.widthM !== null && geometry.widthM > 0)
              && (geometry.left.length <= 1 || geometry.right.length <= 1) && geometry.outline.length > 1 && (
              <Line
                points={flatten(geometry.outline, view)}
                closed
                fill={PALETTE[theme].trackBand}
                stroke={PALETTE[theme].trackBandEdge}
                strokeWidth={1.5}
              />
            )}
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
                drawTrace(ctx._context, { trace: store.trace(), view, theme, select: selectedLap });
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
        </Stage>
      </div>
      {footer && <div className="track-status" role="status">{footer}</div>}
    </div>
  );
}
