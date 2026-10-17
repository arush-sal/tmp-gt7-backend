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
import math
import subprocess
from pathlib import Path

import numpy as np

from rkb.affine import Affine, fit_bbox, predict_affine
from rkb.corners import detect_corners, on_track, select_lap

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

    Unlike VictoryDash TT captures, do NOT force in_race here: feed the rows
    through exactly as decoded so `select_lap` sees the true segmentation via
    `on_track` (older captures set in_race=true; current GT7 never sets it, so
    on_track falls back to current_lap >= 0 + moving).
    """
    rows = _load_rows(csv_path)
    return detect_corners(rows)


def centerline_from_capture(csv_path) -> list:
    """The capture's selected driven lap (`rkb.corners.select_lap`, the same lap the
    corner detector uses) as [[x, z], ...] world metres, starting at the lap rollover
    (the start/finish line).

    This is a DRIVEN RACING LINE, not a surveyed centreline: it is published as the
    boundary's `cl` so live track detection and centreline positioning have dense,
    authoritative world geometry for captured tracks (the SVG outline alone is ~20-30
    points, too sparse for the shape matcher). It must never upgrade fidelity fields.
    """
    rows = _load_rows(csv_path)
    return [[float(rows[i]["pos_x"]), float(rows[i]["pos_z"])] for i in select_lap(rows)]


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
    on_track_rows = [r for r in rows if on_track(r)]
    rows_for_bbox = on_track_rows if on_track_rows else rows
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


def split_capture_by_jumps(rows, jump_m=100.0, min_len=2000) -> list:
    """Split a multi-layout capture's ON-TRACK rows (rkb.corners.on_track) into contiguous driven segments.

    GT7's own lap counter does not reset at a scenery/layout-select boundary --
    only the car's position teleports. So instead of trusting `current_lap`,
    break wherever two consecutive in-race samples' position jumps by more
    than `jump_m` metres (a real driven lap never moves that far in one
    telemetry frame). Segments shorter than `min_len` rows are dropped: a
    teleport itself produces 1-2 point stub segments, and a genuinely partial
    / pit-lane run is not worth ingesting as a layout.
    """
    segments = []
    cur = []
    prev = None
    for r in (r for r in rows if on_track(r)):
        x, z = float(r["pos_x"]), float(r["pos_z"])
        if prev is not None and math.hypot(x - prev[0], z - prev[1]) > jump_m:
            if cur:
                segments.append(cur)
            cur = []
        cur.append(r)
        prev = (x, z)
    if cur:
        segments.append(cur)
    return [s for s in segments if len(s) >= min_len]


def _segment_bbox_center(segment):
    xs = [float(r["pos_x"]) for r in segment]
    zs = [float(r["pos_z"]) for r in segment]
    return ((min(xs) + max(xs)) / 2.0, (min(zs) + max(zs)) / 2.0)


def _signed_area(segment):
    xs = [float(r["pos_x"]) for r in segment]
    zs = [float(r["pos_z"]) for r in segment]
    area = 0.0
    n = len(xs)
    for i in range(n):
        j = (i + 1) % n
        area += xs[i] * zs[j] - xs[j] * zs[i]
    return area / 2.0


def segment_winding_sign(segment) -> int:
    """+1 or -1: sign of the driven path's shoelace signed area.

    Two laps of the SAME physical route driven in opposite directions have
    opposite winding sign. Bbox center alone can't distinguish forward from
    reverse (they cover the same ground) -- this is the tie-breaker once a
    segment has already been matched to a route by bbox center.
    """
    return 1 if _signed_area(segment) >= 0 else -1


def assign_segments_to_slugs(segments, slug_bboxes) -> dict:
    """Match driven segments to candidate slugs by nearest bbox-center.

    `slug_bboxes`: {slug: (cx, cz)}. Slugs whose center is (numerically) the
    same physical spot are a forward/reverse pair for one route -- both of
    the two nearest segments are claimed for the pair and returned under
    EACH slug as the same 2-element list (forward vs reverse unresolved; see
    `resolve_forward_reverse`). A slug with a center no other slug shares
    gets its single nearest segment directly, as a 1-element list.

    Returns {slug: list[segment]}.
    """
    remaining = list(segments)
    groups = []  # [(center, [slugs sharing it])]
    for slug, center in slug_bboxes.items():
        for g in groups:
            gc = g[0]
            if math.hypot(gc[0] - center[0], gc[1] - center[1]) < 1.0:
                g[1].append(slug)
                break
        else:
            groups.append((center, [slug]))

    result = {}
    for center, slugs in groups:
        n = len(slugs)
        remaining.sort(
            key=lambda s: math.hypot(
                _segment_bbox_center(s)[0] - center[0], _segment_bbox_center(s)[1] - center[1]
            )
        )
        claimed, remaining = remaining[:n], remaining[n:]
        for slug in slugs:
            result[slug] = claimed
    return result


def resolve_forward_reverse(assigned, forward_sign_by_slug) -> dict:
    """Flatten `assign_segments_to_slugs`'s output into one segment per slug.

    `forward_sign_by_slug`: {slug: +1 or -1}, the `segment_winding_sign` that
    identifies THIS slug's segment within its (possibly shared) candidate
    list. For a uniquely-matched slug (1-element list) the sign is moot --
    its only candidate is returned as-is. For a forward/reverse pair (shared
    2-element list), the segment whose winding sign matches is picked.
    """
    out = {}
    for slug, segs in assigned.items():
        if len(segs) == 1:
            out[slug] = segs[0]
            continue
        want = forward_sign_by_slug[slug]
        matches = [s for s in segs if segment_winding_sign(s) == want]
        out[slug] = matches[0] if matches else segs[0]
    return out


def _write_rows_csv(rows, csv_path) -> None:
    csv_path = Path(csv_path)
    csv_path.parent.mkdir(parents=True, exist_ok=True)
    with open(csv_path, "w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)


def capture_csvs_for_multi_layout(pcap_path, slug_bboxes, forward_sign_by_slug, csv_out_dir=None) -> dict:
    """Decode a single multi-layout pcap ONCE and split it into one CSV per slug.

    Used when several distinct track layouts were driven back-to-back in one
    capture with no clean lap-counter separation (e.g. Sardegna's 6 road
    layouts): decode -> `split_capture_by_jumps` -> `assign_segments_to_slugs`
    -> `resolve_forward_reverse`, then write each resolved slug's rows to its
    own CSV under `csv_out_dir` (default out/capture-csv/).

    Returns {slug: csv_path}.
    """
    csv_path = decode_capture(pcap_path)
    rows = _load_rows(csv_path)
    segments = split_capture_by_jumps(rows)
    assigned = assign_segments_to_slugs(segments, slug_bboxes)
    resolved = resolve_forward_reverse(assigned, forward_sign_by_slug)

    out_dir = Path(csv_out_dir) if csv_out_dir is not None else (_TOOL_ROOT / "out" / "capture-csv")
    out = {}
    for slug, seg_rows in resolved.items():
        out_path = out_dir / f"{slug}.csv"
        _write_rows_csv(seg_rows, out_path)
        out[slug] = str(out_path)
    return out
