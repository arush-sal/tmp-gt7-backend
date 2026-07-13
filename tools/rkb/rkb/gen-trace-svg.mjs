// Render the driven path from car-coords.csv to a world-coordinate SVG.
// viewBox is set to the world bounding box (1 SVG unit = 1 meter), so the
// output can be overlaid directly on a track SVG for world->SVG correlation.
// NO axis flip is applied (matches VictoryDash's empirical Z->screenY mapping);
// SVG y-down means larger world-Z draws lower. If the trace looks mirrored vs
// the real track image, flip Z here (see FLIP_Z).
import { readFileSync, writeFileSync } from "node:fs";

const FLIP_Z = false; // set true if correlation shows the trace mirrored vertically
const IN = process.argv[2];
const OUT = process.argv[3];

const lines = readFileSync(IN, "utf8").trim().split("\n");
const header = lines[0].split(",");
const ix = {
  lap: header.indexOf("current_lap"),
  inRace: header.indexOf("in_race"),
  x: header.indexOf("posx"),
  z: header.indexOf("posz"),
};

const pts = [];
for (let i = 1; i < lines.length; i++) {
  const c = lines[i].split(",");
  if (c[ix.inRace] !== "true") continue;
  const x = parseFloat(c[ix.x]);
  const z = parseFloat(c[ix.z]);
  if (!Number.isFinite(x) || !Number.isFinite(z)) continue;
  if (x === 0 && z === 0) continue; // skip invalid (VictoryDash convention)
  pts.push([x, z]);
}

let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
for (const [x, z] of pts) {
  if (x < minX) minX = x; if (x > maxX) maxX = x;
  if (z < minZ) minZ = z; if (z > maxZ) maxZ = z;
}
const spanX = maxX - minX;
const spanZ = maxZ - minZ;
const pad = Math.max(spanX, spanZ) * 0.03;

const zf = (z) => (FLIP_Z ? (maxZ - (z - minZ)) : z);

// Pen-up: split the path where consecutive samples jump an implausible distance
// (grid-spawn -> race-start teleport, pit reset). ~400 km/h = ~1.85 m/frame at 60Hz;
// anything many multiples of that is a teleport, not driving.
const JUMP_M = 30;
const runs = [];
let run = [];
for (let i = 0; i < pts.length; i++) {
  if (i > 0) {
    const dx = pts[i][0] - pts[i - 1][0], dz = pts[i][1] - pts[i - 1][1];
    if (Math.hypot(dx, dz) > JUMP_M) { if (run.length) runs.push(run); run = []; }
  }
  run.push(pts[i]);
}
if (run.length) runs.push(run);
const polylines = runs
  .filter((r) => r.length > 1)
  .map((r) => `<polyline points="${r.map(([x, z]) => `${x.toFixed(2)},${zf(z).toFixed(2)}`).join(" ")}" fill="none" stroke="#2da1ff" stroke-width="${Math.max(spanX, spanZ) / 350}" stroke-linejoin="round" stroke-linecap="round"/>`)
  .join("\n");

const vbX = minX - pad;
const vbY = (FLIP_Z ? minZ : minZ) - pad;
const vbW = spanX + pad * 2;
const vbH = spanZ + pad * 2;

// 100m grid in world units for scale reference
let grid = "";
const g0x = Math.ceil((minX - pad) / 100) * 100;
for (let gx = g0x; gx <= maxX + pad; gx += 100)
  grid += `<line x1="${gx}" y1="${vbY}" x2="${gx}" y2="${vbY + vbH}" />`;
const g0z = Math.ceil((minZ - pad) / 100) * 100;
for (let gz = g0z; gz <= maxZ + pad; gz += 100)
  grid += `<line x1="${vbX}" y1="${zf(gz)}" x2="${vbX + vbW}" y2="${zf(gz)}" />`;

const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${vbX.toFixed(2)} ${vbY.toFixed(2)} ${vbW.toFixed(2)} ${vbH.toFixed(2)}" width="1200" height="${Math.round(1200 * vbH / vbW)}">
<g stroke="#888888" stroke-width="${Math.max(spanX, spanZ) / 800}" opacity="0.10">${grid}</g>
${polylines}
<circle cx="${pts[0][0]}" cy="${zf(pts[0][1])}" r="${Math.max(spanX, spanZ) / 120}" fill="#2ed16a"/>
<text x="${vbX + pad}" y="${vbY + pad * 1.5}" fill="#8b949e" font-family="monospace" font-size="${Math.max(spanX, spanZ) / 45}">world m | x:[${minX.toFixed(0)},${maxX.toFixed(0)}] z:[${minZ.toFixed(0)},${maxZ.toFixed(0)}] | ${pts.length} pts | 100m grid | FLIP_Z=${FLIP_Z} | green=start</text>
</svg>
`;
writeFileSync(OUT, svg);
console.log(`wrote ${OUT}: ${pts.length} pts, viewBox ${vbX.toFixed(0)} ${vbY.toFixed(0)} ${vbW.toFixed(0)} ${vbH.toFixed(0)} (world meters)`);
