import csv, json, re
import numpy as np

def parse_svg_outline(svg_text: str):
    m = re.search(r'id="track-outline"[^>]*\bd="([^"]+)"', svg_text)
    d = m.group(1)
    toks = re.findall(r"[MLZ]|-?\d+(?:\.\d+)?", d)
    pts, i = [], 0
    while i < len(toks):
        if toks[i] in ("M", "L"):
            pts.append((float(toks[i+1]), float(toks[i+2]))); i += 3
        else:
            i += 1
    sm = re.search(r'id="track-outline"[^>]*stroke-width="([\d.]+)"', svg_text)
    stroke = float(sm.group(1)) if sm else 0.0
    vb = tuple(float(x) for x in re.search(r'viewBox="([^"]+)"', svg_text).group(1).split())
    return pts, stroke, vb

def load_course(path: str) -> list:
    with open(path) as f:
        return list(csv.DictReader(f))

def load_detect(path: str) -> dict:
    out = {}
    with open(path) as f:
        for r in csv.DictReader(f):
            out[int(r["TRACK"])] = {
                "bbox": (float(r["MINX"]), float(r["MINY"]), float(r["MAXX"]), float(r["MAXY"])),
                "sf": ((float(r["P1X"]), float(r["P1Y"])), (float(r["P2X"]), float(r["P2Y"]))),
                "direction": r["DIRECTION"],
            }
    return out

def load_kb(kb_dir: str) -> dict:
    import glob, os
    out = {}
    for gj in sorted(glob.glob(os.path.join(kb_dir, "*.geometry.json"))):
        slug = os.path.basename(gj)[:-len(".geometry.json")]
        d = json.load(open(gj))
        svg_path = os.path.join(kb_dir, slug + ".svg")
        pts, stroke, vb = parse_svg_outline(open(svg_path).read()) if os.path.exists(svg_path) else ([], 0.0, None)
        out[slug] = {
            "name": d.get("track", slug), "hex_id": d.get("official_track_id"),
            "number_of_turns": d.get("number_of_turns"), "sector_turns": d.get("sector_turns"),
            "sector_labels": d.get("sector_labels", []), "turns": d.get("turns", []),
            "outline_px": pts, "outline_stroke_px": stroke, "viewbox": vb,
        }
    return out

def load_telemetry_csv(path: str) -> np.ndarray:
    return np.genfromtxt(path, delimiter=",", names=True)
