"""RKB corpus validation harness: coordinate-usability tier report and
leave-one-out affine sanity checks."""
from collections import Counter


def report(records) -> dict:
    """Summarize a batch of RKB records: counts per confidence.coordinate_usability
    tier, and the slugs still stuck on the needs-capture backlog."""
    tiers = Counter(r["confidence"]["coordinate_usability"] for r in records)
    backlog = [r["slug"] for r in records if r["confidence"]["coordinate_usability"] == "needs-capture"]
    return {"tiers": dict(tiers), "needs_capture_backlog": backlog, "total": len(records)}


def leave_one_out(fitted):
    """For calibration tracks, predict the affine scale from perimeter/length
    alone and compare it to the fitted (bbox/procrustes) scale.

    `fitted`: list of {slug, fitted_scale, perimeter_px, length_m}.
    Returns a list of {slug, predicted_scale, fitted_scale, scale_err_pct}.
    """
    out = []
    for f in fitted:
        pred = f["perimeter_px"] / f["length_m"]
        out.append({
            "slug": f["slug"],
            "predicted_scale": pred,
            "fitted_scale": f["fitted_scale"],
            "scale_err_pct": abs(pred - f["fitted_scale"]) / f["fitted_scale"] * 100,
        })
    return out
