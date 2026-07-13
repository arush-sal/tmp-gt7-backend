"""Ingestion of OUR OWN GT7 pcap packet captures into RKB pieces.

Unlike a VictoryDash replay (which ships a separate `track.json` with true
measured left/right edges), one of our own captures gives us ONLY a driven
world-coordinate trace -- decoded from the raw `.pcapng` via the Go tool
`pcap2csv` (reuses the backend's packet decoder, same idea as `vd2csv` for
VictoryDash's `telemetry.json`). That driven trace is still AUTHORITATIVE
world coordinates (we drove it ourselves), so it can resolve a needs-capture
track's affine the same way VictoryDash's bbox-from-driven-trace tier does --
but there is no separate measured boundary/width ground truth here, so
boundary/width still come from the SVG outline (derived), not measured.
"""
import csv
import subprocess
from pathlib import Path

import numpy as np

from rkb.affine import Affine, fit_bbox, predict_affine
from rkb.corners import detect_corners

# backend root = .../backend, computed relative to this file
# (this file: backend/tools/rkb/rkb/capture.py)
_BACKEND_ROOT = Path(__file__).resolve().parents[3]

# tool root = .../backend/tools/rkb, computed relative to this file. Decoded
# CSVs default to here (out/capture-csv/), NEVER into supporting-data/ --
# that directory holds read-only input data.
_TOOL_ROOT = Path(__file__).resolve().parents[1]


def decode_capture(pcap_path, csv_out=None) -> str:
    """Decode a raw GT7 pcapng capture into a full-field CSV via pcap2csv.

    Returns the path to the produced CSV (as a str).
    """
    pcap_path = Path(pcap_path)
    if csv_out is None:
        out_dir = _TOOL_ROOT / "out" / "capture-csv"
        out_dir.mkdir(parents=True, exist_ok=True)
        csv_out = out_dir / f"{pcap_path.stem}.csv"
    else:
        csv_out = Path(csv_out)
        csv_out.parent.mkdir(parents=True, exist_ok=True)
    csv_out = str(csv_out)
    subprocess.run(
        ["go", "run", "./tools/rkb/pcap2csv", str(pcap_path), csv_out],
        cwd=str(_BACKEND_ROOT),
        check=True,
    )
    return csv_out


def _load_rows(csv_path) -> list:
    with open(csv_path) as f:
        return list(csv.DictReader(f))


def corners_from_capture(csv_path) -> list:
    """Detect corners on our own capture's clean driven lap (kinematic, via rkb.corners).

    Unlike VictoryDash TT captures, our own captures DO set in_race=true
    correctly for a real race/flying lap -- do NOT force it here, feed the
    rows through exactly as decoded so `select_lap` sees the true segmentation.
    """
    rows = _load_rows(csv_path)
    return detect_corners(rows)


def affine_from_capture(csv_path, outline_px) -> Affine:
    """Fit a world<->px affine from our own capture's driven bbox against a KB SVG outline.

    Same reasoning as `rkb.victorydash.affine_from_vd`'s bbox tier: the SVG is a
    plan-view drawing of the same track at a known uniform scale + translation
    (no rotation), so we bbox-align the driven trace's world bbox onto the
    outline's px bbox rather than running procrustes (which would invent a
    spurious rotation from two independently-sampled, differently-shaped
    polylines -- a raw driven trace is not a clean closed centreline).
    """
    rows = _load_rows(csv_path)
    in_race_rows = [r for r in rows if r.get("in_race") == "true"]
    rows_for_bbox = in_race_rows if in_race_rows else rows
    xs = [float(r["pos_x"]) for r in rows_for_bbox]
    zs = [float(r["pos_z"]) for r in rows_for_bbox]
    world_bbox = (min(xs), min(zs), max(xs), max(zs))
    try:
        aff = fit_bbox(outline_px, world_bbox)
    except ValueError:
        # Anisotropic bbox (axes' scales disagree >5%) -- fall back to the
        # scale-only predictor tier (offset unresolved), same as
        # rkb.victorydash.affine_from_vd's fallback.
        op = np.asarray(outline_px, dtype=float)
        outline_perimeter = (
            float(np.hypot(*np.diff(op, axis=0).T).sum()) if len(op) > 1 else 0.0
        )
        dx = np.diff(xs)
        dz = np.diff(zs)
        trace_length = float(np.hypot(dx, dz).sum()) if len(xs) > 1 else 0.0
        aff = predict_affine(outline_px, outline_perimeter, trace_length)
    aff.source = "capture"
    return aff
