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


# --- on_track: GT7 no longer sets in_race (0x8E bit0); mirror the backend's gate ---
from rkb.corners import on_track


def test_on_track_in_race_true_always_counts():
    assert on_track({"in_race": "true"})
    assert on_track({"in_race": "true", "current_lap": "-1", "car_speed": "0"})


def test_on_track_in_race_false_needs_lap_and_movement():
    assert on_track({"in_race": "false", "current_lap": "0", "car_speed": "120"})
    assert not on_track({"in_race": "false", "current_lap": "-1", "car_speed": "120"})  # menu
    assert not on_track({"in_race": "false", "current_lap": "1", "car_speed": "3"})  # grid/stationary
    assert not on_track({"in_race": "false"})  # fields absent -> not on track


def test_detect_corners_same_when_in_race_is_never_set():
    rows = list(csv.DictReader(open("tests/fixtures/hsr_lap.csv")))
    expected = detect_corners(rows)
    # current GT7: in_race false on every frame, plus menu junk (lap -1) that must be ignored
    menu = [dict(rows[0], in_race="false", current_lap="-1", pos_x="9999", pos_z="9999") for _ in range(50)]
    flipped = menu + [dict(r, in_race="false") for r in rows]
    got = detect_corners(flipped)
    assert [c["apex_world"] for c in got] == [c["apex_world"] for c in expected]
