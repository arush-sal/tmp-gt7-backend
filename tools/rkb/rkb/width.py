import math

# Plausible real-world track-edge-to-edge width band (metres). Cross-sections
# outside this band are not real track width -- on self-crossing tracks
# (figure-8s, hairpins that double back near themselves), nearest-neighbor
# left/right pairing can jump to the OTHER side of the track where it
# approaches itself, producing sub-metre or wildly-oversized "widths".
MIN_PLAUSIBLE_WIDTH_M = 3.0
MAX_PLAUSIBLE_WIDTH_M = 40.0

def from_boundaries(lb, rb, min_m=MIN_PLAUSIBLE_WIDTH_M, max_m=MAX_PLAUSIBLE_WIDTH_M):
    n = min(len(lb), len(rb))
    ws = [math.hypot(lb[i][0]-rb[i][0], lb[i][1]-rb[i][1]) for i in range(n)]
    kept = [w for w in ws if min_m <= w <= max_m]
    n_kept = len(kept)
    return {
        "width_m": round(sum(kept)/n_kept, 2) if kept else None,
        "min_m": round(min(kept), 2) if kept else None,
        "max_m": round(max(kept), 2) if kept else None,
        "source": "victorydash",
        "profile": [round(w, 2) for w in kept],
        "n_total": n,
        "n_kept": n_kept,
    }

def from_svg_stroke(stroke_px, affine):
    return {"width_m": round(stroke_px / affine.scale, 2), "min_m": None, "max_m": None,
            "source": "svg_stroke", "profile": None}

# Below this fraction of surviving (plausible) cross-sections, the true
# boundaries are mostly crossover mis-pairings (e.g. a figure-8 track), not a
# real measurement -- fall back to the SVG-stroke width rather than emit a
# statistic built from a small, unrepresentative remainder.
MIN_KEPT_FRACTION = 0.20

def measure(lb, rb, stroke_px=None, affine=None, min_kept_frac=MIN_KEPT_FRACTION,
            min_m=MIN_PLAUSIBLE_WIDTH_M, max_m=MAX_PLAUSIBLE_WIDTH_M):
    """Measure width from true left/right boundaries, discarding implausible
    crossover-mispaired cross-sections; falls back to the SVG-stroke width
    (when `stroke_px`/`affine` are given) if too few cross-sections survive.
    """
    w = from_boundaries(lb, rb, min_m=min_m, max_m=max_m)
    n_total = w["n_total"]
    frac = (w["n_kept"] / n_total) if n_total else 0.0
    if frac < min_kept_frac and stroke_px is not None and affine is not None:
        return from_svg_stroke(stroke_px, affine)
    return w
