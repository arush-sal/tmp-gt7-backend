from rkb.ids import reconcile, TrackRef

def test_reconcile_joins_by_name_and_disambiguates_reverse():
    kb = {"high-speed-ring": "High Speed Ring", "high-speed-ring-2": "High Speed Ring Reverse"}
    course = [
        {"ID": "11", "Name": "High Speed Ring", "Base": "20", "Length": "4345", "NumCorners": "6", "IsReverse": "0"},
        {"ID": "1069", "Name": "High Speed Ring Reverse", "Base": "20", "Length": "4345", "NumCorners": "6", "IsReverse": "1"},
        {"ID": "1261", "Name": "Special Stage Route X 400", "Base": "5", "Length": "30283", "NumCorners": "0", "IsReverse": "0"},
    ]
    detect = {  # keyed by normalized name -> bbox+sf
        "highspeedring": {"bbox": (-817.0, -391.4, 738.7, 537.6), "sf": ((75.7,-373.7),(75.7,-391.4))},
    }
    refs = reconcile(kb, detect_by_norm=detect, course_rows=course)
    assert set(refs) == {"high-speed-ring", "high-speed-ring-2"}         # phantom SSRX excluded
    assert refs["high-speed-ring"].ddm_id == 11
    assert refs["high-speed-ring"].world_bbox == (-817.0, -391.4, 738.7, 537.6)
    assert refs["high-speed-ring-2"].ddm_id == 1069 and refs["high-speed-ring-2"].is_reverse is True
    assert refs["high-speed-ring"].join == "exact"

def test_reconcile_kb_without_course_row_keeps_slug():
    kb = {"nurburgring-endurance-2": "Nürburgring Endurance II"}
    refs = reconcile(kb, detect_by_norm={}, course_rows=[])
    assert refs["nurburgring-endurance-2"].ddm_id is None
    assert refs["nurburgring-endurance-2"].join == "fuzzy"   # kb-only, no external confirmation


def test_reconcile_fills_base_override_for_unreconciled_venue_member():
    """A KB-only layout absent from course.csv still gets its venue base via _BASE_OVERRIDES,
    so venue-level passes (base-width) include it. join stays fuzzy (no roster confirmation)."""
    kb = {"nurburgring-endurance-2": "Nürburgring Endurance II"}
    refs = reconcile(kb, detect_by_norm={}, course_rows=[])
    assert refs["nurburgring-endurance-2"].base == 24
    assert refs["nurburgring-endurance-2"].join == "fuzzy"


def test_reconcile_course_base_wins_over_override():
    """When a course row supplies the base, it wins -- the override only fills a missing base."""
    kb = {"nurburgring-endurance-2": "Nürburgring Endurance II"}
    course = [{"ID": "9", "Name": "Nürburgring Endurance II", "Base": "99",
               "Length": "25000", "NumCorners": "0", "IsReverse": "1"}]
    refs = reconcile(kb, detect_by_norm={}, course_rows=course)
    assert refs["nurburgring-endurance-2"].base == 99  # course row, not the override
