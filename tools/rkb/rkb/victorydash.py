"""VictoryDash replay ingestion into RKB pieces.

A VictoryDash replay directory (`supporting-data/victorydash/<slug>/`) holds two files:

  track.json      -- true track edges in GT7 world metres: {"cl": [...], "lb": [...], "rb": [...]}
  telemetry.json   -- one driven lap, decrypted GT7 format-C packets (VictoryDash export)

`telemetry.json` is decoded to a full-field CSV by the Go tool `vd2csv` (reuses the
backend's packet decoder), then fed through the existing `rkb.corners` kinematic
detector. `track.json`'s true boundaries/centreline feed `rkb.width` (measured width)
and `rkb.affine` (procrustes fit against KB SVG outline geometry) directly -- no
detector needed for those, they are ground truth.
"""
import csv
import json
import os
import subprocess
from pathlib import Path

import numpy as np

from rkb.affine import Affine, fit_procrustes
from rkb.corners import detect_corners
from rkb.width import from_boundaries

# backend root = .../backend, computed relative to this file
# (this file: backend/tools/rkb/rkb/victorydash.py)
_BACKEND_ROOT = Path(__file__).resolve().parents[3]


def decode_lap(vd_dir, csv_out=None) -> str:
    """Decode a VictoryDash replay's telemetry.json into a full-field CSV via vd2csv.

    Returns the path to the produced CSV (as a str).
    """
    vd_dir = Path(vd_dir)
    telemetry_path = vd_dir / "telemetry.json"
    if csv_out is None:
        csv_out = vd_dir / "telemetry.csv"
    csv_out = str(csv_out)
    subprocess.run(
        ["go", "run", "./tools/rkb/vd2csv", str(telemetry_path), csv_out],
        cwd=str(_BACKEND_ROOT),
        check=True,
    )
    return csv_out


def load_lap_rows(csv_path) -> list:
    """Load vd2csv's CSV rows, forcing in_race="true" (VD omits the flag in TT mode)."""
    with open(csv_path) as f:
        rows = list(csv.DictReader(f))
    for r in rows:
        r["in_race"] = "true"
    return rows


def corners_from_vd(vd_dir) -> list:
    """Detect corners on a VictoryDash replay's driven lap (kinematic, via rkb.corners)."""
    csv_path = decode_lap(vd_dir)
    rows = load_lap_rows(csv_path)
    return detect_corners(rows)


def _nearest_align(left, right):
    """Reorder/resample `right` so right[i] is the nearest point to left[i].

    track.json's lb/rb are independently-sampled polylines (different point
    counts, no shared index), so naive index-pairing drifts wildly out of
    alignment over a lap. Nearest-neighbor pairing keeps each left/right pair
    a true local track-width cross-section.
    """
    L = np.asarray(left, dtype=float)
    R = np.asarray(right, dtype=float)
    aligned = np.empty_like(L)
    for i, p in enumerate(L):
        d2 = (R[:, 0] - p[0]) ** 2 + (R[:, 1] - p[1]) ** 2
        aligned[i] = R[int(np.argmin(d2))]
    return aligned.tolist()


def boundary_from_vd(vd_dir) -> dict:
    """Load true left/right track edges from a VictoryDash replay's track.json.

    `right` is nearest-neighbor aligned to `left` so index-paired consumers
    (e.g. `rkb.width.from_boundaries`) see true local width cross-sections
    rather than drifting index-pairing artifacts.
    """
    with open(Path(vd_dir) / "track.json") as f:
        track = json.load(f)
    left = track["lb"]
    right = _nearest_align(left, track["rb"])
    out = {"source": "victorydash", "left": left, "right": right}
    if "cl" in track:
        out["cl"] = track["cl"]
    return out


def width_from_vd(vd_dir) -> dict:
    """Measured track width from a VictoryDash replay's true left/right edges."""
    boundary = boundary_from_vd(vd_dir)
    return from_boundaries(boundary["left"], boundary["right"])


def affine_from_vd(vd_dir, outline_px) -> Affine:
    """Fit a world<->px affine from a VictoryDash replay's centreline against a KB SVG outline.

    Both `cl` (VictoryDash centreline, world) and `outline_px` (KB SVG outline, px)
    describe the same track path (not the racing line), making them the cleanest
    correspondence available for a procrustes fit.
    """
    with open(Path(vd_dir) / "track.json") as f:
        track = json.load(f)
    aff = fit_procrustes(track["cl"], outline_px)
    aff.source = "victorydash"
    return aff
