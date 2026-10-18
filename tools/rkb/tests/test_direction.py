"""Corner left/right `direction` must match the real track, in GT7's world frame.

GT7's world is right-handed with y UP, so in the horizontal (x, z) plane, seen
from above, +z is 90 deg CLOCKWISE of +x. An increasing velocity heading
atan2(vz, vx) is therefore a RIGHT turn (the opposite of the math-textbook
x-right/y-up plane). Evidence, from real laps: the inner (more-steered) front
wheel and GT7's angular_velocity_y (negative in right-handers) both agree.
"""
import csv
import math
import os

import numpy as np
import pytest

from rkb.affine import Affine
from rkb.corners import corners_from_outline, detect_corners, select_lap, turn_direction

_HERE = os.path.dirname(os.path.abspath(__file__))
_GT7 = os.path.join(_HERE, "..", "..", "..", "..")
_VD = os.path.join(_GT7, "supporting-data", "victorydash")
_CAP_CSV = os.path.join(_HERE, "..", "out", "capture-csv")  # written by `python -m rkb`


def _rows(path, force_in_race=False):
    with open(path) as f:
        rows = list(csv.DictReader(f))
    if force_in_race:  # VictoryDash TT exports omit the flag (see rkb.victorydash.load_lap_rows)
        for r in rows:
            r["in_race"] = "true"
    return rows


def _need(path):
    return pytest.mark.skipif(not os.path.exists(path), reason=f"{path} not available")


# ---- sign convention (synthetic) ----

def test_turn_direction_increasing_heading_is_right():
    assert turn_direction(+0.5) == "right"
    assert turn_direction(-0.5) == "left"


def _synthetic_lap(clockwise_from_above):
    """A ~2.4 km circular lap at 60 Hz in GT7 world coords. Driving east (+x) and
    turning toward +z (south, since z = east x up) is a right turn."""
    n, r, v = 9000, 380.0, 60.0
    sgn = 1.0 if clockwise_from_above else -1.0
    rows = []
    for i in range(n):
        th = sgn * (v / r) * i / 60.0           # heading atan2(vz, vx)
        rows.append({
            "package_id": str(i), "current_lap": "1", "in_race": "true",
            "pos_x": str(r * math.sin(th) * sgn), "pos_z": str(r * (1 - math.cos(th)) * sgn),
            "vel_x": str(v * math.cos(th)), "vel_z": str(v * math.sin(th)),
            "car_speed": str(v * 3.6), "throttle": "50", "brake": "0", "gear": "4", "rot_roll": "0",
        })
    return rows


def test_kinematic_clockwise_lap_is_all_right_turns():
    cs = detect_corners(_synthetic_lap(clockwise_from_above=True))
    assert cs and {c["direction"] for c in cs} == {"right"}


def test_kinematic_counterclockwise_lap_is_all_left_turns():
    cs = detect_corners(_synthetic_lap(clockwise_from_above=False))
    assert cs and {c["direction"] for c in cs} == {"left"}


_SQUARE = [(0, 0), (100, 0), (100, 100), (0, 100)]


def test_outline_corners_have_unknown_direction():
    # SVG drawing order is not the driving direction, so left/right is unknown.
    # The corners themselves (position, turn number) are kept.
    cs = corners_from_outline(_SQUARE, Affine(scale=1.0, tx=0.0, tz=0.0))
    assert [c["turn"] for c in cs] == [1, 2, 3, 4]
    assert [c["apex_world"] for c in cs] == [[0.0, 0.0], [100.0, 0.0], [100.0, 100.0], [0.0, 100.0]]
    assert all(c["direction"] is None for c in cs)
    assert {c["source"] for c in cs} == {"outline_curvature"}


def test_outline_corners_unknown_for_both_drawing_orders():
    aff = Affine(scale=1.0, tx=0.0, tz=0.0)
    for outline in (_SQUARE, _SQUARE[::-1]):
        assert all(c["direction"] is None for c in corners_from_outline(outline, aff))


def test_reverse_of_outline_twin_stays_unknown():
    from rkb.reverse import derive_reverse
    from rkb.schema import validate_record
    from tests.test_reverse import _forward_record, _reverse_ident
    fwd = _forward_record()
    fwd["corners"] = corners_from_outline(_SQUARE, Affine(scale=1.0, tx=0.0, tz=0.0))
    fwd["provenance"]["corners"] = "outline_curvature"
    rev = derive_reverse(fwd, _reverse_ident())
    validate_record(rev)
    assert rev["corners"] and all(c["direction"] is None for c in rev["corners"])


_DB = os.path.join(_HERE, "..", "out", "rkb.db")


@_need(_DB)
def test_built_outline_twins_blue_moon_speedway_are_unknown():
    # Was LLLLL for BOTH the forward and the reverse layout: an artefact of SVG drawing order.
    import sqlite3
    con = sqlite3.connect(_DB)
    for slug in ("blue-moon-speedway", "blue-moon-speedway-2", "red-bull-ring-short", "suzuka-east"):
        dirs = [r[0] for r in con.execute("select direction from corners where slug=?", (slug,))]
        assert dirs and all(d is None for d in dirs), (slug, dirs)
    # no outline-derived corner anywhere carries a direction
    assert con.execute(
        "select count(*) from corners where source='outline_curvature' and direction is not null"
    ).fetchone()[0] == 0


# ---- real laps: GT7's own fields ----

def _assert_matches_yaw_rate(rows):
    """Each kinematic corner's direction == sign of GT7 angular_velocity_y at the apex
    (negative = right, as the inner-wheel check on format-C laps shows)."""
    idx = select_lap(rows)
    X = np.array([float(rows[i]["pos_x"]) for i in idx])
    Z = np.array([float(rows[i]["pos_z"]) for i in idx])
    AVY = np.array([float(rows[i]["angvel_y"]) for i in idx])
    cs = detect_corners(rows)
    assert cs
    for c in cs:
        j = int(np.argmin((X - c["apex_world"][0]) ** 2 + (Z - c["apex_world"][1]) ** 2))
        avy = AVY[max(0, j - 15): j + 16].mean()
        assert c["direction"] == ("left" if avy > 0 else "right"), (c["turn"], avy)


def test_hsr_fixture_directions_match_gt7_yaw_rate():
    _assert_matches_yaw_rate(_rows(os.path.join(_HERE, "fixtures", "hsr_lap.csv")))


SUZUKA_VD = os.path.join(_VD, "week-29-daily-c-ryun")


@_need(SUZUKA_VD)
def test_suzuka_directions_match_inner_front_wheel():
    """Ackermann is sign-convention-free: the inner front wheel steers MORE."""
    from rkb.victorydash import decode_lap
    rows = _rows(decode_lap(SUZUKA_VD), force_in_race=True)
    idx = select_lap(rows)
    X = np.array([float(rows[i]["pos_x"]) for i in idx])
    Z = np.array([float(rows[i]["pos_z"]) for i in idx])
    SL = np.array([abs(float(rows[i]["wheel_steer_l"])) for i in idx])
    SR = np.array([abs(float(rows[i]["wheel_steer_r"])) for i in idx])
    for c in detect_corners(rows):
        j = int(np.argmin((X - c["apex_world"][0]) ** 2 + (Z - c["apex_world"][1]) ** 2))
        w = slice(max(0, j - 15), j + 16)
        inner = "left" if SL[w].mean() > SR[w].mean() else "right"
        assert c["direction"] == inner, c["turn"]


# ---- real laps: ground-truth corners ----

def _vd_dirs(vd_name):
    from rkb.victorydash import decode_lap
    return [c["direction"] for c in detect_corners(_rows(decode_lap(os.path.join(_VD, vd_name)), True))]


def _cap_dirs(slug):
    return [c["direction"] for c in detect_corners(_rows(os.path.join(_CAP_CSV, f"{slug}.csv")))]


@_need(SUZUKA_VD)
def test_suzuka_t1_right_then_s_curves_alternate():
    # detected T1 = First Curve (real T1+T2, one right-hand run); T2.. = the Esses.
    assert _vd_dirs("week-29-daily-c-ryun")[:4] == ["right", "left", "right", "left"]


@_need(os.path.join(_VD, "spa-time-trial-antonio-santos"))
def test_spa_la_source_is_right():
    assert _vd_dirs("spa-time-trial-antonio-santos")[0] == "right"


@_need(os.path.join(_VD, "interlagos-time-trial-a-tapai"))
def test_interlagos_senna_s_entry_is_left():
    assert _vd_dirs("interlagos-time-trial-a-tapai")[0] == "left"


@pytest.mark.parametrize("slug", ["red-bull-ring", "brands-hatch-indy", "brands-hatch-gp"])
def test_t1_is_right(slug):
    path = os.path.join(_CAP_CSV, f"{slug}.csv")
    if slug == "brands-hatch-indy":
        path = os.path.join(_CAP_CSV, "brand-hatch-indy-3laps-3rd-clean.csv")
    if not os.path.exists(path):
        pytest.skip(f"{path} not available")
    assert detect_corners(_rows(path))[0]["direction"] == "right"


@_need(os.path.join(_CAP_CSV, "daytona-tri-oval.csv"))
def test_daytona_tri_oval_counterclockwise_is_all_left():
    assert set(_cap_dirs("daytona-tri-oval")) == {"left"}


# ---- heading_change_deg (DB corner_angle_deg): an UNSIGNED magnitude on every path ----
# Convention: heading_change_deg >= 0 always; the turn's sense lives only in `direction`.

def test_outline_corner_angle_is_unsigned():
    aff = Affine(scale=1.0, tx=0.0, tz=0.0)
    for outline in (_SQUARE, _SQUARE[::-1]):  # either drawing order -> same magnitudes
        angles = [c["heading_change_deg"] for c in corners_from_outline(outline, aff)]
        assert angles == [90.0, 90.0, 90.0, 90.0]


def test_driven_lap_angle_is_magnitude_and_direction_carries_sense():
    cs = detect_corners(_rows(os.path.join(_HERE, "fixtures", "hsr_lap.csv")))
    assert all(c["heading_change_deg"] > 0 for c in cs)
    assert {c["direction"] for c in cs} == {"left", "right"}
    for cw in (True, False):
        for c in detect_corners(_synthetic_lap(clockwise_from_above=cw)):
            assert c["heading_change_deg"] > 0 and c["direction"] == ("right" if cw else "left")


@_need(_DB)
def test_built_corner_angles_are_never_negative():
    import sqlite3
    con = sqlite3.connect(_DB)
    assert con.execute("select count(*) from corners where corner_angle_deg < 0").fetchone()[0] == 0
