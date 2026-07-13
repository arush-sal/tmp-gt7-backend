import math

def from_boundaries(lb, rb):
    n = min(len(lb), len(rb))
    ws = [math.hypot(lb[i][0]-rb[i][0], lb[i][1]-rb[i][1]) for i in range(n)]
    return {"width_m": round(sum(ws)/n, 2), "min_m": round(min(ws), 2), "max_m": round(max(ws), 2),
            "source": "victorydash", "profile": [round(w, 2) for w in ws]}

def from_svg_stroke(stroke_px, affine):
    return {"width_m": round(stroke_px / affine.scale, 2), "min_m": None, "max_m": None,
            "source": "svg_stroke", "profile": None}
