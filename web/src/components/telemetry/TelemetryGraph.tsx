import { useEffect, useLayoutEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import type { Channel, HistoryStore } from "../../lib/historyStore";
import type { LapView } from "../../lib/lapView";
import { CHANNEL_COLOR, MEDIAN_COLOR, REFERENCE_COLOR } from "./graphColors";
import { clampIndex } from "../../lib/trackScrub";
import type { LiveRefAligner } from "../../lib/liveReference";

// Shared cursor-sync group: every telemetry chart joins it (same key) and syncs on the x scale
// only, so hovering one card lines up the crosshair + value readout at the same lap-time x on all
// cards. y is NOT synced (each metric has its own y-range). Module-level singleton = one group for
// the whole telemetry panel.
const CURSOR_SYNC = uPlot.sync("gt7-telemetry");

// uPlot's TS types declare AlignedData as tuples of number[] | TypedArray. columns() returns
// [xs, ...perChannel]; a channel value can be NaN (historyStore uses NaN for a missing sample,
// e.g. delta before a reference lap). uPlot's gap marker is null, and NaN would poison its
// auto-range min/max scan — so map non-finite SERIES values to null (x, index 0, is always finite).
function toAligned(cols: number[][]): uPlot.AlignedData {
  const clean = cols.map((col, i) =>
    i === 0 ? col : col.map(v => (Number.isFinite(v) ? v : null)),
  );
  return clean as unknown as uPlot.AlignedData;
}

// Hover-tooltip value formatting: gear (no unit) reads as an integer; everything else to 1 dp.
function fmtTipValue(v: number, unit: string): string {
  const n = unit === "" ? String(Math.round(v)) : v.toFixed(1);
  return unit ? `${n} ${unit}` : n;
}

interface Props {
  store: HistoryStore;
  channels: Channel[];
  label: string;
  unit?: string;
  id?: string; // applied to the body root so the card's toggle can aria-controls it (no extra wrapper)
  hidden?: string[];     // series keys turned off
  analysisLap?: LapView | null;       // non-null -> analysis mode: show this completed lap, static
  analysisScrubIndex?: number | null; // sample index within the analysis lap to park the cursor on
  // Overlay columns, pre-resampled onto the analysis lap's samples (see lapOverlays). null = absent.
  // Series count is fixed at mount, so the card must be re-keyed when either presence flips.
  medianCol?: number[] | null;
  referenceCol?: number[] | null;
  // Live mode (no analysisLap): reference aligned incrementally to the growing current lap by position
  // (see lib/liveReference). Mutually exclusive with referenceCol; same remount-on-presence rule.
  liveRef?: LiveRefAligner | null;
  // Grafana-style shared zoom (analysis mode only). xRange is the [min,max] lap-time window applied
  // to every graph; null = full lap. A drag-select here calls onXRange with the (5s-min-clamped) window.
  xRange?: [number, number] | null;
  onXRange?: (r: [number, number] | null) => void;
  // Hover readout: instead of a box over the plot, values are lifted to the legend below. Keyed by
  // series label (channel name / "median" / "reference"); null when the cursor leaves.
  onHover?: (h: { x: string; values: Record<string, string> } | null) => void;
}

export type HoverReadout = { x: string; values: Record<string, string> };

/**
 * A single uPlot line chart driven by an imperative HistoryStore, on its own
 * requestAnimationFrame loop (no React re-render per frame — see historyStore/useHistory).
 *
 * Lifecycle (StrictMode-safe):
 *  - construction is deferred until the container reports a non-zero size (ResizeObserver),
 *    so uPlot never inits 0x0 inside a flex/grid before layout settles.
 *  - a `mounted` flag guards the rAF callback so a frame firing during the dev double-mount
 *    teardown/remount window can't call setData on an already-destroyed instance.
 *  - cleanup always runs: cancelAnimationFrame -> ro.disconnect -> u.destroy.
 */
export default function TelemetryGraph({
  store, channels, label, unit, id, hidden, analysisLap, analysisScrubIndex, medianCol, referenceCol, liveRef,
  xRange = null, onXRange, onHover,
}: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const uRef = useRef<uPlot | null>(null);

  // Keep the latest store/channels in refs so the effect below doesn't need to
  // re-run (and re-create the chart) when they change identity across renders.
  const storeRef = useRef(store);
  storeRef.current = store;
  const channelsRef = useRef(channels);
  channelsRef.current = channels;
  const hiddenRef = useRef(hidden ?? []);
  hiddenRef.current = hidden ?? [];
  const unitRef = useRef(unit ?? "");
  unitRef.current = unit ?? "";
  // Analysis mode: a non-null LapView -> show that completed lap's data (static), instead
  // of the live rAF buffer. Kept in a ref so the always-running tick() picks up the latest
  // value without needing to be rebuilt.
  const analysisLapRef = useRef(analysisLap ?? null);
  analysisLapRef.current = analysisLap ?? null;
  const medianColRef = useRef(medianCol ?? null);
  medianColRef.current = medianCol ?? null;
  const referenceColRef = useRef(referenceCol ?? null);
  referenceColRef.current = referenceCol ?? null;
  const liveRefRef = useRef(liveRef ?? null);
  liveRefRef.current = liveRef ?? null;
  const onXRangeRef = useRef(onXRange);
  onXRangeRef.current = onXRange;
  const onHoverRef = useRef(onHover);
  onHoverRef.current = onHover;
  const dpr = typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1;

  // Series keys in DRAW order (uPlot paints later series on top). Reference is the base, then median,
  // then the channel(s)/current line on top. Presence is fixed at mount (the card is re-keyed if it
  // flips), so seriesSpec(), dataCols() and the hidden-toggle effect all read this one ordering.
  const orderedSeriesKeys = (): string[] => [
    ...(referenceColRef.current || liveRefRef.current ? ["reference"] : []),
    ...(medianColRef.current ? ["median"] : []),
    ...channelsRef.current,
  ];

  const strokeFor = (key: string): string =>
    key === "reference" ? REFERENCE_COLOR : key === "median" ? MEDIAN_COLOR : CHANNEL_COLOR[key as Channel];

  // Returns [{key, stroke, dash}] in the order their data columns appear.
  const seriesSpec = () =>
    orderedSeriesKeys().map(key => ({ key, stroke: strokeFor(key), dash: undefined as number[] | undefined }));

  const dataCols = () => {
    const chs = channelsRef.current;
    const lap = analysisLapRef.current;
    if (!lap) {
      const live = storeRef.current.columns(chs); // live: [xs, ...actuals]
      const lr = liveRefRef.current;
      if (!lr) return live;
      const ref = lr.column(chs[0], storeRef.current.trace().current) ?? live[0].map(() => NaN);
      return [live[0], ref, ...live.slice(1)]; // reference drawn first (base), as in analysis mode
    }
    // Analysis mode: static lap columns + pre-resampled overlays, emitted in orderedSeriesKeys order
    // (reference, median, then channels) so the draw order is reference-base -> median -> current-top.
    const base = lap.columns(chs); // [xs, ...perChannel(chs order)]
    const chCol = new Map<string, number[]>();
    chs.forEach((c, i) => chCol.set(c, base[i + 1]));
    const cols: number[][] = [base[0]];
    for (const key of orderedSeriesKeys()) {
      if (key === "reference") cols.push(referenceColRef.current!);
      else if (key === "median") cols.push(medianColRef.current!);
      else cols.push(chCol.get(key)!);
    }
    return cols;
  };

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let mounted = true;
    let rafId = 0;
    let ro: ResizeObserver | null = null;

    // Hover readout: lift the per-series values (at the cursor's data index) up to the legend below
    // the plot instead of drawing a box over the line. Keyed by series label. Fires on the hovered
    // card AND (via the x-sync group) on the others, so every legend shows values at the shared x.
    const onCursor = (u: uPlot) => {
      const cb = onHoverRef.current;
      if (!cb) return;
      const { idx, left } = u.cursor;
      if (idx == null || left == null || left < 0) { cb(null); return; }
      const xv = u.data[0][idx];
      if (xv == null) { cb(null); return; }
      const values: Record<string, string> = {};
      for (let i = 1; i < u.series.length; i++) {
        const s = u.series[i];
        if (!s.show) continue;
        const v = u.data[i]?.[idx];
        if (v == null || !Number.isFinite(v)) continue;
        if (typeof s.label === "string") values[s.label] = fmtTipValue(v as number, unitRef.current);
      }
      cb({ x: `${(xv as number).toFixed(1)}s`, values });
    };

    // Drag-select -> zoom window (analysis mode only). drag.setScale:false means uPlot draws the
    // selection rectangle but does NOT zoom itself; we read the selection, clamp to a 5s minimum
    // span within the lap, and hand the window up so ALL graphs zoom together (Grafana-style).
    const onSelect = (u: uPlot) => {
      const cb = onXRangeRef.current;
      if (!analysisLapRef.current || !cb) return;
      const { left, width } = u.select;
      if (width < 3) return; // a click, not a drag
      let min = u.posToVal(left, "x");
      let max = u.posToVal(left + width, "x");
      if (max < min) { const t = min; min = max; max = t; }
      const xs = u.data[0];
      const dmin = xs?.length ? (xs[0] as number) : min;
      const dmax = xs?.length ? (xs[xs.length - 1] as number) : max;
      if (max - min < 5) { const c = (min + max) / 2; min = c - 2.5; max = c + 2.5; } // enforce 5s min span
      min = Math.max(dmin, min); max = Math.min(dmax, max);
      if (max - min < 5) { // clamped against an edge -> slide a 5s window inside the lap
        if (min <= dmin) { min = dmin; max = Math.min(dmax, dmin + 5); }
        else { max = dmax; min = Math.max(dmin, dmax - 5); }
      }
      u.setSelect({ left: 0, top: 0, width: 0, height: 0 }, false); // clear the highlight rectangle
      cb([min, max]);
    };

    const onDblClick = () => { if (analysisLapRef.current) onXRangeRef.current?.(null); };

    const buildOptions = (width: number, height: number): uPlot.Options => ({
      width,
      height,
      scales: { x: { time: false } },
      legend: { show: false },
      // Crosshair on hover; x drag-SELECT enabled (setScale:false — we apply the zoom ourselves so it
      // can be shared across graphs and clamped to 5s). `sync` links every card's cursor on the x
      // scale so all readouts track the same lap-time position (see CURSOR_SYNC).
      cursor: {
        show: true,
        drag: { x: true, y: false, setScale: false },
        sync: { key: CURSOR_SYNC.key, scales: ["x", null], setSeries: false },
      },
      hooks: { setCursor: [onCursor], setSelect: [onSelect] },
      series: [
        {},
        ...seriesSpec().map(s => ({
          label: s.key,
          stroke: s.stroke,
          dash: s.dash,
          width: 1.5,
          points: { show: false },
          show: !hiddenRef.current.includes(s.key),
        })),
      ],
      axes: [
        { stroke: "#9095a0", grid: { stroke: "rgba(128,128,128,.15)" } },
        { stroke: "#9095a0", grid: { stroke: "rgba(128,128,128,.15)" } },
      ],
    });

    const init = (width: number, height: number) => {
      if (uRef.current) return;
      const u = new uPlot(
        buildOptions(width, height),
        toAligned(dataCols()),
        container,
      );
      uRef.current = u;

      // Double-click resets the shared zoom to the full lap (broadcast to all graphs).
      u.over.addEventListener("dblclick", onDblClick);

      const tick = () => {
        if (!mounted) return;
        // Live mode: resetScales true so the x/y axes auto-follow the growing buffer. Analysis mode:
        // data is static and the user may have zoomed, so DON'T reset the scale each frame — the
        // xRange effect owns the x-scale (full lap or the shared zoom window).
        u.setData(toAligned(dataCols()), !analysisLapRef.current);
        rafId = requestAnimationFrame(tick);
      };
      rafId = requestAnimationFrame(tick);
    };

    ro = new ResizeObserver(entries => {
      if (!mounted) return; // a queued resize firing after cleanup must not construct an orphan uPlot
      const entry = entries[0];
      if (!entry) return;
      const { width, height } = entry.contentRect;
      if (width === 0 || height === 0) return;
      if (!uRef.current) {
        init(width, height);
      } else {
        uRef.current.setSize({ width, height });
      }
    });
    ro.observe(container);

    return () => {
      mounted = false;
      cancelAnimationFrame(rafId);
      ro?.disconnect();
      uRef.current?.destroy();
      uRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Dev guard: series are built once at mount; changing channels/median without a remount
  // (new key) would desync series metadata from the per-frame data columns.
  const builtKey = useRef<string | null>(null);
  useEffect(() => {
    const k = channels.join(",");
    if (builtKey.current === null) builtKey.current = k;
    else if (builtKey.current !== k && import.meta.env?.DEV) {
      console.warn(`TelemetryGraph: channels/median changed after mount (${builtKey.current} -> ${k}). Key this component by its card to force a remount.`);
    }
  }, [channels]);

  // Apply the shared zoom window to this graph's x-scale (analysis mode only). null -> fit the full
  // lap. Runs on lap change too (deps) so a new selection refits. The tick() leaves scales alone in
  // analysis mode, so this effect owns the x-scale there.
  useLayoutEffect(() => {
    const u = uRef.current;
    if (!u || !analysisLap) return;
    const xs = u.data[0];
    if (!xs || !xs.length) return;
    if (xRange) u.setScale("x", { min: xRange[0], max: xRange[1] });
    else u.setScale("x", { min: xs[0] as number, max: xs[xs.length - 1] as number });
  }, [xRange, analysisLap]);

  // Analysis mode: park the crosshair at the scrub sample (instead of following the mouse).
  // Reads the lap columns directly (not u.data) so the cursor is correct on the same render the
  // lap/scrub index changes, without waiting for the next rAF tick to catch up.
  useLayoutEffect(() => {
    const u = uRef.current;
    if (!u || !analysisLap) return;
    const xs = analysisLap.columns(channels)[0];
    if (!xs || xs.length === 0) return;
    const idx = clampIndex(analysisScrubIndex ?? 0, xs.length);
    const xv = xs[idx];
    if (!Number.isFinite(xv)) return;
    const left = u.valToPos(xv, "x");
    const top = u.bbox.height / 2 / dpr;
    u.setCursor({ left, top });
  }, [channels, analysisLap, analysisScrubIndex, dpr]);

  // Reflect `hidden` onto the built chart without rebuilding it. Covers the channel series plus the
  // median/reference overlays, in the same order dataCols() emits them.
  useLayoutEffect(() => {
    const u = uRef.current;
    if (!u) return;
    orderedSeriesKeys().forEach((key, i) => u.setSeries(i + 1, { show: !(hidden ?? []).includes(key) }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hidden]);

  return (
    <div className="telemetry-graph__body" id={id}>
      <div
        className="telemetry-graph__canvas"
        ref={containerRef}
        role="img"
        aria-label={unit ? `${label} (${unit})` : label}
      />
    </div>
  );
}
