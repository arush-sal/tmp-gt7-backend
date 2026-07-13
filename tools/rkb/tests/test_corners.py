import csv
from rkb.corners import detect_corners
from rkb.corners import corners_from_stored
from rkb.affine import Affine


def detect_corners_from_csv(path):
    rows = list(csv.DictReader(open(path)))
    return detect_corners(rows)


def test_detects_six_corners_on_hsr_lap():
    corners = detect_corners_from_csv("tests/fixtures/hsr_lap.csv")
    assert len(corners) == 6
    # T1 apex is the banked loop centre near (732,-261), sane g (not the 8g entry spike)
    t1 = corners[0]
    assert t1["lat_accel_g"] < 4.0
    assert t1["direction"] in ("left", "right")


def test_apex_is_heading_midpoint_not_entry():
    corners = detect_corners_from_csv("tests/fixtures/hsr_lap.csv")
    # apex x within the loop's far side, not the entry (< 700 would be the entry spike)
    assert corners[0]["apex_world"][0] > 700


def test_corners_from_stored_maps_px_to_world():
    aff = Affine(scale=0.5, tx=100.0, tz=200.0)
    turns = [{"turn": 10, "x": 200, "y": 300}]
    cs = corners_from_stored(turns, aff)
    assert cs[0]["turn"] == 10
    assert cs[0]["source"] == "stored" and cs[0]["apex_is_label"] is True
    assert cs[0]["apex_world"] == [ (200-100)/0.5, (300-200)/0.5 ]  # [200.0, 200.0]
