"""
Scalar track-width extraction from a GT7 format-C "width lap" using surface_type
(4 per-wheel codes FL,FR,RL,RR: T=tarmac C=curb D=dirt G=grass S=sand).

A "width lap" is a lap where the driver deliberately hugs the left then right
track limit, so a wheel frequently reads off-tarmac. The track limit is the
tarmac->off transition: a LEFT-edge event is a LEFT wheel (FL or RL) off
tarmac while the car body is at the left limit; a RIGHT-edge event is a RIGHT
wheel (FR or RR) off tarmac. A sample with wheels off on BOTH sides is
ambiguous and skipped.

CHOSEN SCOPE (see vault spec "Track Width from Surface Type Design v1", the
"CHOSEN SCOPE" section): a single SCALAR width per track, not a per-station
variable model. This module does not touch db.py/schema.py -- it only
produces a width dict matching the shape db.py already consumes
({width_m,min_m,max_m,source,n_total,n_kept,profile}); wiring it into
build_all is a later task.

Scalar width = median(|left offsets|) + median(|right offsets|), i.e. the
distance from the car-centre driven line to the left limit plus the distance
from the car-centre driven line to the right limit. This measures
car-CENTRE-to-limit on each side, not true track edge-to-edge (a per-car
half-track correction is applied later, in calibration) -- so the raw left/
right medians are also returned in `profile` for that later step. No
half-track constant is hardcoded here.
"""
import math

TARMAC = "T"

# Guard thresholds for the self-crossing / mis-projection guard (step 6).
# A projected edge point is rejected if it lands implausibly far from the
# spine (mis-match / far-flung telemetry glitch) or if the car's heading at
# that sample disagrees with the local spine-segment heading (dot < 0.5),
# which indicates a bad nearest-point pairing (e.g. a self-crossing track
# snapping to the wrong branch).
MAX_SPINE_DISTANCE_M = 15.0
MIN_HEADING_DOT = 0.5

# Moving-average window (samples) used to smooth the provisional spine when
# no external centreline is supplied. GT7 telemetry is ~60 Hz, so 1001
# samples spans ~16.7s -- long enough to average a full corner's worth of
# left+right hugging excursions toward a more central path, but short enough
# to stay local (a much larger window starts blending in unrelated, distant
# sections of a curving track and drags the spine off the local track
# direction -- caught by the self-crossing guard below, but better avoided).
SPINE_SMOOTH_WINDOW = 1001


def _moving_average(points, window):
    n = len(points)
    if n == 0:
        return []
    half = window // 2
    out = []
    for i in range(n):
        lo = max(0, i - half)
        hi = min(n, i + half + 1)
        chunk = points[lo:hi]
        sx = sum(p[0] for p in chunk) / len(chunk)
        sz = sum(p[1] for p in chunk) / len(chunk)
        out.append((sx, sz))
    return out


def _build_spine(rows_in_race):
    """Provisional spine = the lap's own driven path, smoothed."""
    path = [(float(r["pos_x"]), float(r["pos_z"])) for r in rows_in_race]
    return _moving_average(path, SPINE_SMOOTH_WINDOW)


def _parse_wheels(surface_type):
    """surface_type string -> (FL, FR, RL, RR) codes, or None if malformed."""
    if not surface_type or len(surface_type) != 4:
        return None
    return surface_type[0], surface_type[1], surface_type[2], surface_type[3]


def _classify_edge(codes):
    """Returns 'left', 'right', or None (no edge event / ambiguous)."""
    fl, fr, rl, rr = codes
    left_off = fl != TARMAC or rl != TARMAC
    right_off = fr != TARMAC or rr != TARMAC
    if left_off and right_off:
        return None  # ambiguous -- skip
    if left_off:
        return "left"
    if right_off:
        return "right"
    return None  # fully on tarmac -- not an edge event


def _nearest_spine_index(spine, x, z):
    """Brute-force nearest spine vertex to (x, z). Returns (index, distance)."""
    best_i, best_d2 = 0, None
    for i, (sx, sz) in enumerate(spine):
        d2 = (sx - x) ** 2 + (sz - z) ** 2
        if best_d2 is None or d2 < best_d2:
            best_d2, best_i = d2, i
    return best_i, math.sqrt(best_d2) if best_d2 is not None else float("inf")


def _spine_segment_heading(spine, i):
    """Unit tangent of the spine at vertex i (using the neighbouring segment)."""
    n = len(spine)
    if n < 2:
        return None
    j = i + 1 if i + 1 < n else i - 1
    if j == i:
        return None
    dx = spine[j][0] - spine[i][0]
    dz = spine[j][1] - spine[i][1]
    mag = math.hypot(dx, dz)
    if mag == 0:
        return None
    return dx / mag, dz / mag


def _median(values):
    if not values:
        return None
    s = sorted(values)
    n = len(s)
    mid = n // 2
    if n % 2 == 1:
        return s[mid]
    return (s[mid - 1] + s[mid]) / 2.0


def width_from_surface(rows, centreline=None):
    """Scalar track width from a surface_type width-lap.

    `rows` -- list of dict rows (csv.DictReader-style, string values) with at
    least: pos_x, pos_z, vel_x, vel_z, surface_type, in_race, current_lap.
    `centreline` -- optional list of [x, z] points to use as the spine
    (e.g. VictoryDash `cl`); if omitted, a provisional spine is built from the
    lap's own smoothed driven path.

    Returns {width_m, min_m, max_m, source, n_total, n_kept, profile} where
    profile carries the raw left/right components + provenance flags (see
    module docstring / spec) needed for later calibration.
    """
    in_race_rows = [r for r in rows if r.get("in_race") == "true"]

    spine = [(float(p[0]), float(p[1])) for p in centreline] if centreline else _build_spine(in_race_rows)

    left_offsets = []
    right_offsets = []
    n_total = 0
    n_kept = 0

    for r in in_race_rows:
        codes = _parse_wheels(r.get("surface_type"))
        if codes is None:
            continue
        side = _classify_edge(codes)
        if side is None:
            continue  # ambiguous (both sides off) or fully on tarmac

        n_total += 1

        if not spine:
            continue

        x = float(r["pos_x"])
        z = float(r["pos_z"])
        vx = float(r.get("vel_x", 0.0))
        vz = float(r.get("vel_z", 0.0))

        idx, dist = _nearest_spine_index(spine, x, z)
        if dist > MAX_SPINE_DISTANCE_M:
            continue  # self-crossing / mis-projection guard

        seg_heading = _spine_segment_heading(spine, idx)
        speed = math.hypot(vx, vz)
        if seg_heading is not None and speed > 0:
            hx, hz = vx / speed, vz / speed
            dot = hx * seg_heading[0] + hz * seg_heading[1]
            if abs(dot) < MIN_HEADING_DOT:
                continue  # heading disagrees with spine segment -- bad pairing

        # Per-sample heading from velocity (atan2(vz, vx)); left unit normal
        # is the heading rotated +90 degrees. Sign convention is internally
        # consistent (left/right already separated by `side`); only the
        # magnitude matters for the final scalar width.
        if speed > 0:
            hx, hz = vx / speed, vz / speed
        elif seg_heading is not None:
            hx, hz = seg_heading
        else:
            continue
        normal = (-hz, hx)

        sx, sz = spine[idx]
        offset = (x - sx) * normal[0] + (z - sz) * normal[1]

        n_kept += 1
        if side == "left":
            left_offsets.append(offset)
        else:
            right_offsets.append(offset)

    left_abs = [abs(v) for v in left_offsets]
    right_abs = [abs(v) for v in right_offsets]
    left_med = _median(left_abs)
    right_med = _median(right_abs)

    if left_med is not None and right_med is not None:
        width_m = round(left_med + right_med, 3)
    else:
        width_m = None

    # min_m/max_m: spread from per-station cross-sections isn't attempted in
    # the scalar-only scope (no per-station pairing); fall back to
    # median +- IQR of the combined |offset| pool when there's enough spread
    # to compute an IQR, else leave unset.
    combined = left_abs + right_abs
    min_m = max_m = None
    if len(combined) >= 4:
        s = sorted(combined)
        n = len(s)
        q1 = s[n // 4]
        q3 = s[(3 * n) // 4]
        iqr = q3 - q1
        if width_m is not None:
            min_m = round(max(0.0, width_m - iqr), 3)
            max_m = round(width_m + iqr, 3)

    coverage_pct = round(100.0 * n_kept / n_total, 1) if n_total else 0.0

    return {
        "width_m": width_m,
        "min_m": min_m,
        "max_m": max_m,
        "source": "telemetry_width_lap",
        "n_total": n_total,
        "n_kept": n_kept,
        "profile": {
            "left_offset_median": round(left_med, 3) if left_med is not None else None,
            "right_offset_median": round(right_med, 3) if right_med is not None else None,
            "n_left": len(left_abs),
            "n_right": len(right_abs),
            "coverage_pct": coverage_pct,
            "publishable_measured": False,
            "calibration_id": None,
        },
    }
