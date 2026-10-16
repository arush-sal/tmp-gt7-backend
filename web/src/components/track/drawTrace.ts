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

// The predicted (reference) lap's drive-line: a single flat VIOLET solid stroke — deliberately NOT
// the per-segment throttle/brake colouring (and no brake sawtooth), so it reads as one clearly
// distinct line beside the driver's green/red/amber/white line. Matches REFERENCE_COLOR (graphs).
// The predicted lap is spliced from fastest-sector segments of different laps, so its position jumps
// at splice boundaries (measured ~69 m in 0.1 s ≈ 678 m/s) — break the line there with a TIGHT
// teleport ceiling (100 m/s -> 400 m/s w/ penUpBreaks' 4x margin; the driver line's 200 was too loose
// and let the ~82 m threshold pass the 69 m jump) so no straight diagonal is drawn across the track.
const REFERENCE_LINE_COLOR = "#F433FF";
function drawReferenceLine(ctx: CanvasRenderingContext2D, pts: ReadonlyArray<TracePoint>, view: View) {
  if (pts.length < 2) return;
  // Detect splice teleports on the FULL sample array, where dt is the true per-frame gap (~0.016-0.1s)
  // -> the ~69 m jump reads as ~700 m/s and is caught. Decimating FIRST inflated the kept-point dt
  // (dropped straight points) so the speed threshold rose and the jump slipped through -> diagonal.
  const fullDts = pts.map((p, k) =>
    k === 0 ? 0 : (p.t !== undefined && pts[k - 1].t !== undefined ? p.t - pts[k - 1].t! : NaN));
  const breaks = penUpBreaks(pts, fullDts, 100); // original indices where a teleport starts
  const keep = decimate(pts, view, 1);
  ctx.save();
  ctx.strokeStyle = REFERENCE_LINE_COLOR;
  ctx.lineWidth = 2;
  ctx.globalAlpha = 0.9;
  ctx.lineJoin = "round";
  ctx.beginPath();
  let started = false;
  let prev = -1;
  for (const idx of keep) {
    const p = pts[idx];
    if (!Number.isFinite(p.posX) || !Number.isFinite(p.posZ)) { started = false; prev = idx; continue; }
    // A teleport anywhere in the original samples this kept-segment spans -> start a new sub-path.
    if (prev >= 0) { for (let b = prev + 1; b <= idx; b++) if (breaks.has(b)) { started = false; break; } }
    const { sx, sy } = worldToScreen(view, p.posX, p.posZ);
    if (!started) { ctx.moveTo(sx, sy); started = true; } else ctx.lineTo(sx, sy);
    prev = idx;
  }
  ctx.stroke();
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
  referenceLap?: readonly TracePoint[] | null; // predicted lap; drawn beneath the driver line
  overlayLaps?: ReadonlyArray<readonly TracePoint[]>; // "all" mode: every completed lap, faint
}) {
  const select = opts.select ?? "all";
  // Reference line first, so the driver's line sits on top of it.
  if (opts.referenceLap && opts.referenceLap.length > 1) drawReferenceLine(ctx, opts.referenceLap, opts.view);
  if (select === "all") {
    // Overlay every completed lap faintly; fall back to the live buffer when none are loaded yet.
    if (opts.overlayLaps?.length) {
      for (const lap of opts.overlayLaps) drawLap(ctx, lap, opts.view, opts.theme, 0.4);
    } else {
      drawLap(ctx, opts.trace.current, opts.view, opts.theme, 1);
    }
  } else if (select === "current") {
    drawLap(ctx, opts.trace.current, opts.view, opts.theme, 1);
  } else {
    drawLap(ctx, opts.analysisLap ?? [], opts.view, opts.theme, 1);
  }
}
