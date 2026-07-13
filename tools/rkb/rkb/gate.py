import math

def corner_count_ok(n_detected, n_expected, source) -> bool:
    if n_expected in (None, 0): return False
    if source == "kinematic": return abs(n_detected - n_expected) <= 1
    if source == "stored": return n_detected == n_expected
    return abs(n_detected - n_expected) / n_expected <= 0.10  # outline_curvature

def coordinate_usability(affine, join, corner_match) -> str:
    if affine.tx is None or (isinstance(affine.tx, float) and math.isnan(affine.tx)):
        return "needs-capture"
    if join != "exact":
        return "generated-coarse"
    ok_affine = (
        (affine.source == "victorydash" and (affine.rms_m is not None and affine.rms_m <= 5))
        or (affine.source in ("procrustes",) and (affine.rms_m is not None and affine.rms_m <= 8))
        or (affine.source == "bbox_from_driven_trace"
            and (affine.sf_anchor_err_m is not None and affine.sf_anchor_err_m <= 10)
            and (affine.scale_sanity_pct is not None and affine.scale_sanity_pct <= 3))
    )
    if ok_affine and corner_match:
        return "generated-reliable"
    return "generated-coarse"

def physical_boundary_fidelity(boundary_source, width_source) -> str:
    if boundary_source == "victorydash" and width_source == "victorydash":
        return "measured"
    if boundary_source in ("outline",) or width_source in ("svg_stroke",):
        return "derived"
    return "none"
