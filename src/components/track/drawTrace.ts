import type { TracePoint } from "../../lib/historyStore";
import { worldToScreen, type View } from "../../lib/trackTransform";
import { traceColor, brakeSpans } from "../../lib/traceColor";
import { decimate, penUpBreaks } from "../../lib/traceDecimate";

const TOOTH_STEP = 7; // px, fixed arc-length step between sawtooth vertices
const TOOTH_AMP = 3;  // px, perpendicular offset amplitude

// Resample a screen-space polyline at a fixed arc-length step, offsetting each
// vertex perpendicular to the local segment direction by alternating +/- amp.
function sawtoothPoints(pts: ReadonlyArray<{ sx: number; sy: number }>, step: number, amp: number):
  Array<{ x: number; y: number }> {
  if (pts.length < 2) return [];
  const segLen: number[] = [];
  let total = 0;
  for (let i = 1; i < pts.length; i++) {
    const l = Math.hypot(pts[i].sx - pts[i - 1].sx, pts[i].sy - pts[i - 1].sy);
    segLen.push(l);
    total += l;
  }
  if (total <= 0) return [];
  const out: Array<{ x: number; y: number }> = [];
  let segIdx = 0, segStart = 0, tooth = 0;
  for (let d = 0; d <= total; d += step) {
    while (segIdx < segLen.length - 1 && d > segStart + segLen[segIdx]) {
      segStart += segLen[segIdx];
      segIdx++;
    }
    const a = pts[segIdx], b = pts[segIdx + 1];
    const dx = b.sx - a.sx, dy = b.sy - a.sy;
    const len = Math.hypot(dx, dy) || 1;
    const t = Math.max(0, Math.min(1, (d - segStart) / len));
    const px = a.sx + dx * t, py = a.sy + dy * t;
    const nx = -dy / len, ny = dx / len;
    const sign = tooth % 2 === 0 ? 1 : -1;
    out.push({ x: px + nx * amp * sign, y: py + ny * amp * sign });
    tooth++;
  }
  return out;
}

// Regular sawtooth ribbon drawn on top of the racing line, confined to brake spans.
function drawBrakeSawtooth(ctx: CanvasRenderingContext2D, keptPts: ReadonlyArray<TracePoint>,
                          spans: ReadonlyArray<[number, number]>, view: View, alpha: number) {
  if (spans.length === 0) return;
  ctx.save();
  ctx.globalAlpha = alpha;
  ctx.strokeStyle = "rgba(225,6,0,0.9)";
  ctx.lineWidth = 2.2;
  ctx.lineCap = "round";
  ctx.lineJoin = "round";
  for (const [start, end] of spans) {
    if (end - start < 1) continue; // need at least 2 points to have a direction
    const screenPts = [];
    for (let i = start; i <= end; i++) screenPts.push(worldToScreen(view, keptPts[i].posX, keptPts[i].posZ));
    const teeth = sawtoothPoints(screenPts, TOOTH_STEP, TOOTH_AMP);
    if (teeth.length < 2) continue;
    ctx.beginPath();
    ctx.moveTo(teeth[0].x, teeth[0].y);
    for (let i = 1; i < teeth.length; i++) ctx.lineTo(teeth[i].x, teeth[i].y);
    ctx.stroke();
  }
  ctx.restore();
}

function drawLap(ctx: CanvasRenderingContext2D, pts: ReadonlyArray<TracePoint>, view: View,
                theme: "light" | "dark", alpha: number) {
  if (pts.length < 2) return;
  const keep = decimate(pts, view, 1);
  const keptPts = keep.map(i => pts[i]);
  // REAL elapsed time between consecutive kept points (from the samples' lap-time `t`), not a
  // uniform 60Hz guess. Kept points are decimated (live) or thinned to 1500/lap (after a
  // refresh), so a straight-line high-speed segment can span many frames; using 1/60 made the
  // teleport guard fire on those and drop the line (notably the fast run to start/finish).
  // Falls back to 1/60 when `t` is absent (test fixtures) via penUpBreaks' own NaN guard.
  const dts = keptPts.map((p, k) =>
    k === 0 ? 0 : (p.t !== undefined && keptPts[k - 1].t !== undefined ? p.t - keptPts[k - 1].t! : NaN));
  const breaks = penUpBreaks(keptPts, dts, 200);
  const spans = brakeSpans(keptPts.map(p => p.brake));
  ctx.save();
  ctx.globalAlpha = alpha;
  for (let k = 1; k < keep.length; k++) {
    if (breaks.has(k)) continue; // pen-up: skip this segment
    const a = pts[keep[k - 1]], b = pts[keep[k]];
    const pa = worldToScreen(view, a.posX, a.posZ), pb = worldToScreen(view, b.posX, b.posZ);
    ctx.strokeStyle = traceColor(b.throttle, b.brake, theme, alpha);
    ctx.lineWidth = 3;
    ctx.beginPath();
    ctx.moveTo(pa.sx, pa.sy);
    ctx.lineTo(pb.sx, pb.sy);
    ctx.stroke();
  }
  ctx.restore();
  drawBrakeSawtooth(ctx, keptPts, spans, view, alpha); // on top of the racing line, brake spans only
}

export function drawTrace(ctx: CanvasRenderingContext2D, opts: {
  trace: { current: readonly TracePoint[] };
  view: View; theme: "light" | "dark";
  select?: "all" | "current" | number;
  analysisLap?: readonly TracePoint[];
}) {
  const select = opts.select ?? "all";
  // "all" and "current" both plot the in-progress lap at full strength. (Completed laps are no
  // longer retained by the store, so prior laps are no longer faint-overlaid — an accepted minor
  // regression.) A numeric selection isolates the passed analysis lap for analysis.
  if (select === "all" || select === "current") {
    drawLap(ctx, opts.trace.current, opts.view, opts.theme, 1);
  } else {
    drawLap(ctx, opts.analysisLap ?? [], opts.view, opts.theme, 1);
  }
}
