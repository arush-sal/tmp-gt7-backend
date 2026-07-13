from dataclasses import dataclass
from typing import Optional
import math

def bbox_corners(points):
    xs = [p[0] for p in points]; ys = [p[1] for p in points]
    return (min(xs), min(ys), max(xs), max(ys))

@dataclass
class Affine:
    scale: float
    tx: float
    tz: float
    rot_deg: float = 0.0
    source: str = "bbox"
    rms_m: Optional[float] = None
    sf_anchor_err_m: Optional[float] = None
    scale_sanity_pct: Optional[float] = None
    world_bbox: Optional[tuple] = None
    svg_bbox: Optional[tuple] = None
    def world_to_px(self, x, z):
        return (self.scale * x + self.tx, self.scale * z + self.tz)
    def px_to_world(self, px, py):
        return ((px - self.tx) / self.scale, (py - self.tz) / self.scale)

def fit_bbox(outline_px, world_bbox) -> Affine:
    minx, minz, maxx, maxz = world_bbox
    pminx, pminy, pmaxx, pmaxy = bbox_corners(outline_px)
    sx = (pmaxx - pminx) / (maxx - minx)
    sz = (pmaxy - pminy) / (maxz - minz)
    if abs(sx - sz) / max(abs(sx), abs(sz)) > 0.05:
        raise ValueError(f"anisotropic scale sx={sx:.4f} sz={sz:.4f} (>5%) — not a uniform affine")
    s = (sx + sz) / 2
    tx = pminx - s * minx
    tz = pminy - s * minz
    return Affine(scale=s, tx=tx, tz=tz, rot_deg=0.0, source="bbox",
                  scale_sanity_pct=abs(sx - sz) / max(abs(sx), abs(sz)) * 100,
                  world_bbox=world_bbox, svg_bbox=(pminx, pminy, pmaxx, pmaxy))

def sf_anchor_error_m(affine: Affine, sf_line_world, svg_sf_px) -> Optional[float]:
    if svg_sf_px is None or sf_line_world is None:
        return None
    (a, b) = sf_line_world
    mid_world = ((a[0] + b[0]) / 2, (a[1] + b[1]) / 2)
    px, py = affine.world_to_px(*mid_world)
    # svg_sf_px is the S/F midpoint in px; error back in metres
    dpx = math.hypot(px - svg_sf_px[0], py - svg_sf_px[1])
    return dpx / affine.scale

import numpy as np

def fit_procrustes(driven_world_pts, outline_px) -> Affine:
    """Similarity fit world->px by matching the two point sets' shapes (centroid + scale + rotation).
    Both sets describe the same track shape; we align their normalized forms."""
    W = np.asarray(driven_world_pts, float); P = np.asarray(outline_px, float)
    # match counts by resampling the larger to the smaller via linear index sampling
    n = min(len(W), len(P))
    Wi = W[np.linspace(0, len(W)-1, n).astype(int)]
    Pi = P[np.linspace(0, len(P)-1, n).astype(int)]
    wc, pc = Wi.mean(0), Pi.mean(0)
    W0, P0 = Wi - wc, Pi - pc
    # optimal rotation+scale via SVD of cross-covariance
    H = W0.T @ P0
    U, S, Vt = np.linalg.svd(H)
    d = np.ones(H.shape[0])
    if np.linalg.det(Vt.T @ U.T) < 0:      # forbid reflection (no flip)
        d[-1] = -1.0
    R = (Vt.T * d) @ U.T
    scale = float((S * d).sum() / (W0**2).sum())
    rot = math.degrees(math.atan2(R[1,0], R[0,0]))
    t = pc - scale * (R @ wc)
    aff = Affine(scale=scale, tx=float(t[0]), tz=float(t[1]), rot_deg=rot, source="procrustes")
    pred = (scale * (R @ W0.T).T + pc)
    aff.rms_m = float(np.sqrt(((pred - Pi)**2).sum(1).mean())) / scale
    return aff

def assert_no_rotation(affine: Affine, tol_deg: float = 2.0) -> bool:
    return abs(affine.rot_deg) <= tol_deg

def predict_affine(outline_px, outline_perimeter_px, length_m) -> Affine:
    # Last-resort tier: when the source data needed even for a scale-only estimate is
    # missing (no course.csv join -> length_m is None, or no outline traced), leave scale
    # unresolved too rather than crashing — gate.coordinate_usability already treats a NaN
    # tx/scale as "needs-capture", which is the correct (lowest) confidence tier for this.
    if not length_m or not outline_perimeter_px:
        scale = math.nan
    else:
        scale = outline_perimeter_px / length_m
    return Affine(scale=scale, tx=math.nan, tz=math.nan, rot_deg=0.0, source="predictor")

def choose_affine(inp: dict) -> Affine:
    """Pick the best-available affine, in source-priority order:
    1. VictoryDash boundary procrustes (true track edges — best).
    2. gt7trackdetect world bbox (fit_bbox already tags source="bbox"). This bbox is
       itself derived from the driven trace envelope (provenance: bbox_from_driven_trace) —
       it must NEVER be validated against the same telemetry that produced it. The only
       independent check available is the S/F line anchor (svg_sf_px + sf_line), which is
       stored on sf_anchor_err_m when both are supplied.
    3. Telemetry procrustes — only valid when the driven points are NOT the same source
       that fed a bbox above (caller guarantees this by construction of `inp`).
    4. Predictor — scale only, offset unresolved (last resort).

    A higher tier that is *present but fails its own internal sanity check* (e.g. fit_bbox's
    anisotropic-scale guard) is treated the same as "unavailable": we fall through to the
    next tier rather than aborting the whole record. The sanity check itself is not loosened.
    """
    if inp.get("victorydash_pts") is not None and inp.get("outline_px"):
        aff = fit_procrustes(inp["victorydash_pts"], inp["outline_px"])
        aff.source = "victorydash"
        return aff
    if inp.get("world_bbox") is not None and inp.get("outline_px"):
        try:
            aff = fit_bbox(inp["outline_px"], inp["world_bbox"])
        except ValueError:
            aff = None
        if aff is not None:
            aff.source = "bbox_from_driven_trace"  # bias provenance: bbox comes from the driven-trace envelope
            if inp.get("sf_line") is not None and inp.get("svg_sf_px") is not None:
                aff.sf_anchor_err_m = sf_anchor_error_m(aff, inp["sf_line"], inp["svg_sf_px"])
            return aff
    if inp.get("driven_world_pts") is not None and inp.get("outline_px"):
        return fit_procrustes(inp["driven_world_pts"], inp["outline_px"])
    return predict_affine(inp.get("outline_px", []), inp.get("outline_perimeter_px"), inp.get("length_m"))
