import { useEffect, useLayoutEffect, useRef } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import type { Channel, HistoryStore } from "../../lib/historyStore";
import { CHANNEL_COLOR, MEDIAN_COLOR } from "./graphColors";
import { clampIndex } from "../../lib/trackScrub";
// REFERENCE_COLOR (cyan, see graphColors.ts) is the intended stroke for the reference series
// once it ships from the backend RKB. Not imported here — no reference data column exists yet.

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
  median?: boolean;      // single-series only: draw a median trace
  hidden?: string[];     // series keys turned off
  analysisLapIndex?: number | null;   // non-null -> analysis mode: show this completed lap, static
  analysisScrubIndex?: number | null; // sample index within the analysis lap to park the cursor on
}

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
  store, channels, label, unit, id, median, hidden, analysisLapIndex, analysisScrubIndex,
}: Props) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const uRef = useRef<uPlot | null>(null);

  // Keep the latest store/channels in refs so the effect below doesn't need to
  // re-run (and re-create the chart) when they change identity across renders.
  const storeRef = useRef(store);
  storeRef.current = store;
  const channelsRef = useRef(channels);
  channelsRef.current = channels;
  const medianRef = useRef(median);
  medianRef.current = median;
  const hiddenRef = useRef(hidden ?? []);
  hiddenRef.current = hidden ?? [];
  const unitRef = useRef(unit ?? "");
  unitRef.current = unit ?? "";
  // Analysis mode: non-null lap index -> show that completed lap's data (static), instead
  // of the live rAF buffer. Kept in refs so the always-running tick() picks up the latest
  // value without needing to be rebuilt.
  const analysisLapIndexRef = useRef(analysisLapIndex ?? null);
  analysisLapIndexRef.current = analysisLapIndex ?? null;
  const dpr = typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1;

  // Returns [{key, stroke, dash}] in the order their data columns appear.
  const seriesSpec = () => {
    const chs = channelsRef.current;
    if (medianRef.current && chs.length === 1) {
      return [
        { key: "actual", stroke: CHANNEL_COLOR[chs[0]], dash: undefined as number[] | undefined },
        { key: "median", stroke: MEDIAN_COLOR, dash: undefined as number[] | undefined },
      ];
    }
    return chs.map(c => ({ key: c as string, stroke: CHANNEL_COLOR[c], dash: undefined as number[] | undefined }));
  };

  const dataCols = () => {
    const chs = channelsRef.current;
    const lapIdx = analysisLapIndexRef.current;
    if (lapIdx != null) {
      // Analysis mode: a static snapshot of the selected completed lap, not the live buffer.
      // No median overlay -- fill a NaN (-> null, gap) column so the series/data shapes stay
      // identical to live mode without needing a remount.
      const lapCols = storeRef.current.lapColumns(lapIdx, chs);
      if (!lapCols) return [[], ...chs.map(() => []), ...(medianRef.current && chs.length === 1 ? [[]] : [])];
      if (medianRef.current && chs.length === 1) {
        return [lapCols[0], lapCols[1], lapCols[1].map(() => NaN)];
      }
      return lapCols;
    }
    const cols = storeRef.current.columns(chs); // [xs, ...actuals]
    if (medianRef.current && chs.length === 1) {
      return [cols[0], cols[1], storeRef.current.medianColumn(chs[0])];
    }
    return cols;
  };

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let mounted = true;
    let rafId = 0;
    let ro: ResizeObserver | null = null;
    let tip: HTMLDivElement | null = null; // hover readout, created in init(), attached to u.over

    // Fills/positions the hover tooltip from the cursor's data index. Values are read from the
    // live data columns, so the readout is exact for whatever the rAF loop last plotted.
    const onCursor = (u: uPlot) => {
      if (!tip) return;
      const { idx, left, top } = u.cursor;
      if (idx == null || left == null || left < 0) { tip.style.display = "none"; return; }
      const xv = u.data[0][idx];
      if (xv == null) { tip.style.display = "none"; return; }
      // Build with safe DOM methods (no innerHTML): all text goes through textContent, so even
      // though every value here is our own trusted palette/label/number, there is no HTML sink.
      const frag = document.createDocumentFragment();
      const xEl = document.createElement("span");
      xEl.className = "telemetry-graph__tip-x";
      xEl.textContent = `${(xv as number).toFixed(1)}s`;
      frag.appendChild(xEl);
      let any = false;
      for (let i = 1; i < u.series.length; i++) {
        const s = u.series[i];
        if (!s.show) continue;
        const v = u.data[i]?.[idx];
        if (v == null || !Number.isFinite(v)) continue;
        any = true;
        const row = document.createElement("span");
        row.className = "telemetry-graph__tip-row";
        const sw = document.createElement("i");
        if (typeof s.stroke === "string") sw.style.background = s.stroke;
        row.appendChild(sw);
        // The actual series' internal key is "actual" but the legend labels it "current" — match it.
        const dispLabel = s.label === "actual" ? "current" : (s.label ?? "");
        row.appendChild(document.createTextNode(`${dispLabel} ${fmtTipValue(v as number, unitRef.current)}`));
        frag.appendChild(row);
      }
      if (!any) { tip.style.display = "none"; return; }
      tip.replaceChildren(frag);
      tip.style.display = "block";
      // Measure the rendered box and clamp on BOTH axes so it never clips against the card's
      // overflow:hidden — place to the cursor's right/below by default, flip left/above near an
      // edge, then hard-clamp into u.over. (offsetWidth read forces a reflow, negligible here.)
      const pad = 10;
      const ow = u.over.clientWidth, oh = u.over.clientHeight;
      const tw = tip.offsetWidth, th = tip.offsetHeight;
      // On sync RECEIVER cards (not under the mouse) y isn't synced, so `top` is uPlot's
      // proportional mirror of the source card's mouse-height ratio, not this card's data point —
      // fine for placing the box (x is what's meaningful); the row VALUES are the card's own.
      const ty0 = top ?? 0;
      let tx = left + pad; if (tx + tw > ow) tx = left - tw - pad; if (tx < 0) tx = 0;
      let ty = ty0 + pad; if (ty + th > oh) ty = ty0 - th - pad; if (ty < 0) ty = 0;
      tip.style.transform = "none";
      tip.style.left = `${tx}px`;
      tip.style.top = `${ty}px`;
    };

    const buildOptions = (width: number, height: number): uPlot.Options => ({
      width,
      height,
      scales: { x: { time: false } },
      legend: { show: false },
      // Crosshair on hover; drag-zoom disabled (the rAF loop auto-rescales a live chart, so a
      // drag-zoom would just snap back and feel broken). `sync` links every card's cursor on the
      // x scale so all readouts track the same lap-time position (see CURSOR_SYNC).
      cursor: {
        show: true,
        drag: { x: false, y: false },
        sync: { key: CURSOR_SYNC.key, scales: ["x", null], setSeries: false },
      },
      hooks: { setCursor: [onCursor] },
      series: [
        {},
        ...seriesSpec().map(s => ({
          label: s.key,
          stroke: s.stroke,
          dash: s.dash,
          width: 1.5,
          points: { show: false },
          show: s.key === "actual" ? true : !hiddenRef.current.includes(s.key),
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

      // Hover readout: a positioned box inside uPlot's overlay (u.over), so cursor.left/top map
      // directly to its coordinates. Torn down automatically when u.destroy() removes u.over.
      tip = document.createElement("div");
      tip.className = "telemetry-graph__tip";
      tip.style.display = "none";
      u.over.appendChild(tip);

      const tick = () => {
        if (!mounted) return;
        // resetScales left at default (true) so the x/y axes auto-follow the growing buffer;
        // passing false froze the scale at the empty seed range -> the line rendered off-view.
        u.setData(toAligned(dataCols()));
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
    const k = `${channels.join(",")}|${median ? 1 : 0}`;
    if (builtKey.current === null) builtKey.current = k;
    else if (builtKey.current !== k && import.meta.env?.DEV) {
      console.warn(`TelemetryGraph: channels/median changed after mount (${builtKey.current} -> ${k}). Key this component by its card to force a remount.`);
    }
  }, [channels, median]);

  // Analysis mode: park the crosshair at the scrub sample (instead of following the mouse).
  // Reads lapColumns directly (not u.data) so the cursor is correct on the same render the
  // lap/scrub index changes, without waiting for the next rAF tick to catch up.
  useLayoutEffect(() => {
    const u = uRef.current;
    if (!u || analysisLapIndex == null) return;
    const lapCols = store.lapColumns(analysisLapIndex, channels);
    const xs = lapCols?.[0];
    if (!xs || xs.length === 0) return;
    const idx = clampIndex(analysisScrubIndex ?? 0, xs.length);
    const xv = xs[idx];
    if (!Number.isFinite(xv)) return;
    const left = u.valToPos(xv, "x");
    const top = u.bbox.height / 2 / dpr;
    u.setCursor({ left, top });
  }, [store, channels, analysisLapIndex, analysisScrubIndex, dpr]);

  // Reflect `hidden` onto the built chart without rebuilding it.
  useLayoutEffect(() => {
    const u = uRef.current;
    if (!u) return;
    const spec = medianRef.current && channelsRef.current.length === 1
      ? ["actual", "median"]
      : channelsRef.current.map(c => c as string);
    spec.forEach((key, i) => u.setSeries(i + 1, { show: key === "actual" ? true : !(hidden ?? []).includes(key) }));
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
