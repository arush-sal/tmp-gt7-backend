"""Per-car half-track calibration for `rkb.width_surface`'s scalar width-lap measurement.

CHOSEN SCOPE (vault spec "Track Width from Surface Type Design v1"): `width_from_surface`
measures car-CENTRE-to-limit on each side, not true edge-to-edge width, because a wheel
going off-track puts the car's centre only part way to the true limit. This module
calibrates that correction from the ONE track that has both a width-lap capture AND an
independent VictoryDash-measured true width (`sardegna-road-a`), keyed by car_category
(the correction is a property of the CAR's own physical track width, not the circuit) --
and applies it only to width-lap captures using the SAME car/class. With a single anchor,
this calibration is inherently low-confidence (`confidence: "low"` on every entry); it is
NOT cross-validated against a second VictoryDash-measured track for the same car.

This module does not touch db.py/schema.py/the Go store. `calibrated_width` returns a
width dict in the EXACT shape those already consume ({width_m, min_m, max_m, source,
n_total, n_kept, profile}) -- `profile` here is always None (this is the scalar CHOSEN
SCOPE, not a per-station model; the Go store's Width.Profile is a `*[]float64`, so a rich
provenance dict there would break the JSON<->DB roundtrip). Calibration provenance
(w_true_m/w_raw_m/correction_m/confidence, publishable_measured) has no persisted field in
the current record shape and is reported at build time only (see rkb.build), not stored
per-track.
"""
import math

from .victorydash import boundary_from_vd

# Cross-section euclidean distances beyond this are crossover/mis-alignment artifacts, not
# real track width -- the same guard band already used by rkb.width's victorydash
# measurement (rkb.width.MAX_PLAUSIBLE_WIDTH_M).
MAX_PLAUSIBLE_WIDTH_M = 40.0

# A width-lap capture segment shorter than this is a stub (a teleport/pit-lane fragment
# from split_capture_by_jumps, or a partial out/in lap), not a real lap -- excluded from
# the width-lap selection below regardless of its off-tarmac event count.
DEFAULT_MIN_LAP_ROWS = 1500


def _median(values):
    if not values:
        return None
    s = sorted(values)
    n = len(s)
    mid = n // 2
    if n % 2:
        return s[mid]
    return (s[mid - 1] + s[mid]) / 2.0


def vd_width(vd_dir, max_m=MAX_PLAUSIBLE_WIDTH_M):
    """True track width: median nearest-neighbour-aligned lb<->rb distance from a
    VictoryDash replay's track.json, dropping >max_m outlier cross-sections (same guard
    style already used for victorydash width -- see rkb.width.from_boundaries)."""
    boundary = boundary_from_vd(vd_dir)
    left, right = boundary["left"], boundary["right"]
    dists = [math.hypot(l[0] - r[0], l[1] - r[1]) for l, r in zip(left, right)]
    kept = [d for d in dists if d <= max_m]
    return _median(kept)


def build_calibration(anchor_vd_dir, anchor_raw_width_m, car_category, anchor_slug):
    """One calibration-table entry: the half-track correction for `car_category`, derived
    from the ONE anchor track (`anchor_slug`) that has both a VictoryDash true width and a
    width-lap raw measurement for this car. `correction_m` is added to a raw
    `width_from_surface` width_m to approximate true edge-to-edge width."""
    w_true = vd_width(anchor_vd_dir)
    correction = round(w_true - anchor_raw_width_m, 3)
    return {
        "car_category": car_category,
        "anchor_slug": anchor_slug,
        "w_true_m": round(w_true, 3),
        "w_raw_m": round(anchor_raw_width_m, 3),
        "correction_m": correction,
        # Only one anchor track exists -- not cross-validated against a second
        # VictoryDash-measured track for this car, so confidence stays low.
        "confidence": "low",
    }


def calibrated_width(raw, car_category, calibration_table, min_n_kept=30):
    """Apply a per-car half-track correction to a `width_from_surface` raw result.

    Returns (width_dict, publishable_measured):
    - `width_dict` is the DB/schema/Go-safe shape ({width_m, min_m, max_m, source,
      n_total, n_kept, profile}); `profile` is always None (scalar CHOSEN SCOPE).
    - `publishable_measured` is True only when `car_category` matches a calibration
      entry (the correction was actually derived from a capture of the SAME car/class)
      AND the raw measurement is usable (width_m is not None) AND enough cross-sections
      survived the guards (n_kept >= min_n_kept) to trust the number.

    When the car doesn't match any calibration entry, the raw (uncorrected) width is
    returned as-is -- per spec, do not guess a correction for a car/class we have no
    anchor for.
    """
    width_m, min_m, max_m = raw["width_m"], raw["min_m"], raw["max_m"]
    n_kept = raw.get("n_kept") or 0
    entry = calibration_table.get(car_category) if calibration_table else None
    calibrated = entry is not None and width_m is not None
    if calibrated:
        corr = entry["correction_m"]
        width_m = round(width_m + corr, 3)
        if min_m is not None:
            min_m = round(min_m + corr, 3)
        if max_m is not None:
            max_m = round(max_m + corr, 3)
    publishable_measured = bool(calibrated and n_kept >= min_n_kept)
    out = {
        "width_m": width_m,
        "min_m": min_m,
        "max_m": max_m,
        "source": raw["source"],
        "n_total": raw.get("n_total"),
        "n_kept": raw.get("n_kept"),
        "profile": None,
    }
    return out, publishable_measured


def select_width_lap(rows, min_lap_rows=DEFAULT_MIN_LAP_ROWS):
    """Pick the one lap, within a multi-lap width-lap capture segment, that is the
    deliberate edge-hugging width lap: the `current_lap` value with the most
    `surface_type` off-tarmac samples, among laps with at least `min_lap_rows` samples
    (drops brief out-lap/in-lap/teleport stub segments regardless of their off-tarmac
    fraction). Returns that lap's rows (unfiltered further -- `width_from_surface` does
    its own in_race filtering); [] if no lap meets `min_lap_rows`.
    """
    by_lap = {}
    for r in rows:
        by_lap.setdefault(r.get("current_lap"), []).append(r)

    best_lap, best_off = None, -1
    for lap, lap_rows in by_lap.items():
        if len(lap_rows) < min_lap_rows:
            continue
        off = sum(1 for r in lap_rows if r.get("surface_type") and set(r["surface_type"]) - {"T"})
        if off > best_off:
            best_off, best_lap = off, lap

    return by_lap.get(best_lap, [])
