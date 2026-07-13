"""Record-driven SVG overlay renderer + PNG screenshot helper.

Ported from the prototype `render_kinematic.py` (relocated to
`tests/fixtures`-adjacent scratch during Task 0), which hardcoded HSR-specific
inverse-affine constants, official corner label positions, and sector label
positions read off a reference image for one track. This version is
record-driven: the base outline, corner apexes, and sector labels all come
from the RKB record itself (already in world metres), so it works for any
track in the corpus. The segment-colouring-by-throttle/brake trace logic and
the outward-offset corner-label placement are carried over unchanged.
"""
from __future__ import annotations

import json
import math
import os
import subprocess
import tempfile


def _bbox(points):
    xs = [p[0] for p in points]
    zs = [p[1] for p in points]
    return min(xs), max(xs), min(zs), max(zs)


def overlay_svg(record: dict, telemetry_rows=None) -> str:
    """Render an RKB record to an SVG overlay: base outline, numbered corner
    markers (offset outward from the track centroid), sector labels, and (if
    `telemetry_rows` is given) a throttle/brake-coloured driven trace.

    `telemetry_rows` is a list of dicts with at least pos_x, pos_z, throttle,
    brake (as produced by csv.DictReader over a telemetry capture), already
    filtered/selected to the lap of interest by the caller.
    """
    outline = record.get("boundary", {}).get("outline") or []
    corners = record.get("corners") or []
    sectors = record.get("sectors") or []

    pts_for_bbox = [tuple(p) for p in outline]
    pts_for_bbox += [tuple(c["apex_world"]) for c in corners if c.get("apex_world")]
    pts_for_bbox += [tuple(s["label_world"]) for s in sectors if s.get("label_world")]
    if telemetry_rows:
        pts_for_bbox += [(float(r["pos_x"]), float(r["pos_z"])) for r in telemetry_rows]

    if not pts_for_bbox:
        pts_for_bbox = [(0.0, 0.0)]

    minx, maxx, minz, maxz = _bbox(pts_for_bbox)
    pad = max(maxx - minx, maxz - minz) * 0.1 or 10.0
    vbx, vbz = minx - pad, minz - pad
    vbw, vbh = (maxx - minx) + 2 * pad, (maxz - minz) + 2 * pad
    sc = max(vbw, vbh) or 1.0

    cx = (minx + maxx) / 2
    cz = (minz + maxz) / 2

    base = ""
    if len(outline) > 1:
        pts = " ".join(f"{p[0]:.2f},{p[1]:.2f}" for p in outline)
        base = (f'<polyline points="{pts}" fill="none" stroke="#30363d" '
                f'stroke-width="{sc / 200}" stroke-linejoin="round"/>')

    trace = ""
    if telemetry_rows:
        X = [float(r["pos_x"]) for r in telemetry_rows]
        Z = [float(r["pos_z"]) for r in telemetry_rows]
        T = [float(r.get("throttle", 0)) for r in telemetry_rows]
        B = [float(r.get("brake", 0)) for r in telemetry_rows]
        segs = []
        for i in range(len(X) - 1):
            r = int(min(255, B[i] / 100 * 255))
            g = int(min(255, T[i] / 100 * 255))
            col = "#4a5568" if (r < 40 and g < 40) else f"rgb({max(r, 50)},{max(g, 50) if g > 40 else 50},60)"
            segs.append(f'<line x1="{X[i]:.1f}" y1="{Z[i]:.1f}" x2="{X[i + 1]:.1f}" y2="{Z[i + 1]:.1f}" '
                        f'stroke="{col}" stroke-width="{sc / 280}"/>')
        trace = "".join(segs)

    marks = ""
    for c in corners:
        a = c.get("apex_world")
        if not a:
            continue
        ax, az = a
        turn = c.get("turn", "?")
        dx, dz = ax - cx, az - cz
        length = math.hypot(dx, dz) or 1
        off = sc / 16
        lx, lz = ax + dx / length * off, az + dz / length * off
        marks += f'<circle cx="{ax}" cy="{az}" r="{sc / 260}" fill="#fff"/>'
        marks += f'<line x1="{ax}" y1="{az}" x2="{lx}" y2="{lz}" stroke="#888" stroke-width="{sc / 900}"/>'
        marks += (f'<text x="{lx}" y="{lz}" text-anchor="middle" dominant-baseline="middle" '
                  f'font-family="monospace" font-weight="700" font-size="{sc / 26}" fill="#fff">{turn}</text>')

    sect = ""
    for s in sectors:
        label_world = s.get("label_world")
        name = s.get("sector")
        if not label_world or not name:
            continue
        lx, lz = label_world
        sect += (f'<text x="{lx:.0f}" y="{lz:.0f}" text-anchor="middle" font-family="monospace" '
                 f'font-weight="700" font-size="{sc / 24}" fill="#b14fff" font-style="italic">{name}</text>')

    title = record.get("name") or record.get("slug") or ""
    height = round(1200 * vbh / vbw) if vbw else 1200
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="{vbx:.0f} {vbz:.0f} {vbw:.0f} {vbh:.0f}" width="1200" height="{height}">
<rect x="{vbx:.0f}" y="{vbz:.0f}" width="{vbw:.0f}" height="{vbh:.0f}" fill="#0b0e13"/>
{base}{trace}{sect}{marks}
<text x="{vbx + 40:.0f}" y="{vbz + 50:.0f}" font-family="monospace" font-size="{sc / 50}" fill="#8b949e">{title} | line: green=throttle red=brake grey=coast | numbers=corner turn | sector labels in purple</text>
</svg>'''


def _cached_chromium() -> str:
    cache = os.path.expanduser("~/.cache/ms-playwright")
    if os.path.isdir(cache):
        for name in sorted(os.listdir(cache)):
            if name.startswith("chromium-"):
                candidate = os.path.join(cache, name, "chrome-linux64", "chrome")
                if os.path.exists(candidate):
                    return candidate
    raise FileNotFoundError("no cached chromium found under ~/.cache/ms-playwright")


def _default_node_modules() -> str:
    # tools/rkb has no node deps of its own; reuse the sibling frontend's
    # playwright-core (same pattern as the prototype/session shot.mjs).
    return os.path.abspath(
        os.path.join(os.path.dirname(__file__), "..", "..", "..", "..", "frontend", "node_modules")
    )


def save_png(svg: str, path: str, *, width: int = 1200, height: int = 760, node_modules: str | None = None) -> None:
    """Screenshot an SVG string to a PNG by shelling out to node + playwright-core:
    write the SVG to a temp file, load it in a headless Chromium page via the
    cached chromium under ~/.cache/ms-playwright, and screenshot — same pattern
    as the prototype/session's shot.mjs.
    """
    exe = _cached_chromium()
    node_modules = node_modules or _default_node_modules()
    out_path = os.path.abspath(path)

    with tempfile.TemporaryDirectory() as tmp:
        svg_path = os.path.join(tmp, "overlay.svg")
        script_path = os.path.join(tmp, "shot.mjs")
        # Node's ESM resolver doesn't honour NODE_PATH; symlink node_modules
        # into the temp dir so the ordinary node_modules lookup finds
        # playwright-core (tools/rkb has no node deps of its own).
        if os.path.isdir(node_modules):
            os.symlink(node_modules, os.path.join(tmp, "node_modules"))
        with open(svg_path, "w") as f:
            f.write(svg)
        script = f'''import {{ chromium }} from "playwright-core";
import {{ readFileSync }} from "node:fs";
const svg = readFileSync({json.dumps(svg_path)}, "utf8");
const b = await chromium.launch({{executablePath: {json.dumps(exe)}}});
const p = await b.newPage({{viewport: {{width: {width}, height: {height}}}}});
await p.setContent(`<body style="margin:0;background:#0b0e13">${{svg}}</body>`);
await p.screenshot({{path: {json.dumps(out_path)}}});
await b.close();
'''
        with open(script_path, "w") as f:
            f.write(script)
        subprocess.run(["node", script_path], check=True, cwd=tmp)
