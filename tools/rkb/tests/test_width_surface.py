import csv
import math
import os

from rkb.width_surface import width_from_surface

FIXTURE = os.path.join(os.path.dirname(__file__), "fixtures", "gvs_width_sample.csv")


def _load_fixture():
    with open(FIXTURE) as f:
        return list(csv.DictReader(f))


def test_width_from_surface_grand_valley_south_plausible():
    rows = _load_fixture()
    w = width_from_surface(rows)

    assert w["source"] == "telemetry_width_lap"
    assert 8.0 < w["width_m"] < 25.0
    assert w["n_kept"] > 50
    assert w["profile"]["n_left"] > 0
    assert w["profile"]["n_right"] > 0
    assert w["profile"]["publishable_measured"] is False
    assert w["profile"]["calibration_id"] is None


def test_width_from_surface_synthetic_straight_spine():
    # Straight spine along x-axis at z=0, heading +x. Plant left/right edge
    # points at known perpendicular offsets from the spine.
    spine = [[float(x), 0.0] for x in range(0, 101, 2)]

    rows = []
    # LEFT edge events (a left wheel off tarmac) at z = +3.0 (offset 3.0 m)
    for x in range(10, 90, 4):
        rows.append({
            "pos_x": str(float(x)), "pos_z": "3.0",
            "vel_x": "10.0", "vel_z": "0.0",
            "surface_type": "DTTT",  # FL off (D=dirt)
            "in_race": "true", "current_lap": "1",
        })
    # RIGHT edge events at z = -4.0 (offset 4.0 m)
    for x in range(10, 90, 4):
        rows.append({
            "pos_x": str(float(x)), "pos_z": "-4.0",
            "vel_x": "10.0", "vel_z": "0.0",
            "surface_type": "TDTT",  # FR off (D=dirt)
            "in_race": "true", "current_lap": "1",
        })

    w = width_from_surface(rows, centreline=spine)

    assert w["source"] == "telemetry_width_lap"
    assert w["profile"]["n_left"] > 0
    assert w["profile"]["n_right"] > 0
    # known left offset 3.0 + known right offset 4.0 == 7.0
    assert math.isclose(w["width_m"], 7.0, abs_tol=0.2)


def test_width_from_surface_ambiguous_both_sides_off_is_skipped():
    spine = [[float(x), 0.0] for x in range(0, 101, 2)]
    rows = [{
        "pos_x": "50.0", "pos_z": "0.0",
        "vel_x": "10.0", "vel_z": "0.0",
        "surface_type": "DTDT",  # FL and RL off -> ambiguous (both left wheels)
        "in_race": "true", "current_lap": "1",
    }]
    # Not actually ambiguous by definition (both are LEFT wheels) -- use a genuine
    # both-sides-off case: FL (left) and FR (right) both off.
    rows = [{
        "pos_x": "50.0", "pos_z": "0.0",
        "vel_x": "10.0", "vel_z": "0.0",
        "surface_type": "DDTT",  # FL off (left) AND FR off (right) -> ambiguous
        "in_race": "true", "current_lap": "1",
    }]
    w = width_from_surface(rows, centreline=spine)
    assert w["n_total"] == 0
    assert w["n_kept"] == 0
