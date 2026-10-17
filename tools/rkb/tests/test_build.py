import csv
import math

from rkb.build import (
    _apply_base_width_pass,
    _log_vd_surface_crosscheck,
    _resolve_capture_width,
    build_track,
)
from rkb.ids import TrackRef
from rkb.schema import validate_record


def test_build_track_hsr_from_bbox_and_telemetry():
    import rkb.sources as S

    kb = S.load_kb("tests/fixtures/kb_hsr")  # fixture dir with the one hsr-2 geometry+svg
    ref = TrackRef(
        slug="high-speed-ring-2",
        name="High Speed Ring Reverse",
        ddm_id=1069,
        num_corners=6,
        is_reverse=True,
        world_bbox=(-817.0, -391.4, 738.7, 537.6),
        sf_line=((75.7, -373.7), (75.7, -391.4)),
        join="exact",
    )
    # in_race is a bool STRING ("true"/"false") in this telemetry capture, so we load rows
    # with csv.DictReader (not load_telemetry_csv, which numpy-parses columns and would choke
    # on that field).
    with open("tests/fixtures/hsr_lap.csv") as f:
        rows = list(csv.DictReader(f))

    rec = build_track(ref, kb["high-speed-ring-2"], telemetry_rows=rows, victorydash=None)

    assert rec["affine"]["source"] == "bbox_from_driven_trace"
    assert len(rec["corners"]) == 6 and rec["corners"][0]["source"] == "kinematic"
    assert rec["confidence"]["coordinate_usability"] in ("generated-reliable", "generated-coarse")
    validate_record(rec)  # no raise


def _synthetic_width_lap_rows(car_category="GR1"):
    # Straight spine along x-axis at z=0; left edge events at z=+3.0, right at z=-4.0
    # (known true car-centre offsets 3.0 + 4.0 = 7.0m) -- same pattern as
    # test_width_surface's synthetic straight-spine case, tagged with a car_category.
    rows = []
    for x in range(10, 90, 4):
        rows.append({
            "pos_x": str(float(x)), "pos_z": "3.0", "vel_x": "10.0", "vel_z": "0.0",
            "surface_type": "DTTT", "in_race": "true", "current_lap": "1",
            "car_category": car_category,
        })
    for x in range(10, 90, 4):
        rows.append({
            "pos_x": str(float(x)), "pos_z": "-4.0", "vel_x": "10.0", "vel_z": "0.0",
            "surface_type": "TDTT", "in_race": "true", "current_lap": "1",
            "car_category": car_category,
        })
    return rows


def test_resolve_capture_width_prefers_calibrated_telemetry_when_usable():
    default = {"width_m": 9.0, "min_m": None, "max_m": None, "source": "svg_stroke",
               "n_total": None, "n_kept": None, "profile": None}
    rows = _synthetic_width_lap_rows()
    table = {"GR1": {"correction_m": 1.0, "anchor_slug": "sardegna-road-a", "confidence": "low"}}

    out = _resolve_capture_width(default, rows, table)

    assert out["source"] == "telemetry_width_lap"
    assert math.isclose(out["width_m"], 8.0, abs_tol=0.2)  # 7.0 raw + 1.0 correction
    assert out["profile"] is None


def test_resolve_capture_width_falls_back_to_default_when_no_width_lap_rows():
    default = {"width_m": 9.0, "min_m": None, "max_m": None, "source": "svg_stroke",
               "n_total": None, "n_kept": None, "profile": None}

    assert _resolve_capture_width(default, None, {}) is default
    assert _resolve_capture_width(default, [], {}) is default


def test_resolve_capture_width_falls_back_when_surface_measurement_unusable():
    default = {"width_m": 9.0, "min_m": None, "max_m": None, "source": "svg_stroke",
               "n_total": None, "n_kept": None, "profile": None}
    # No edge events at all -> width_from_surface can't measure anything -> keep default.
    rows = [{
        "pos_x": "0.0", "pos_z": "0.0", "vel_x": "10.0", "vel_z": "0.0",
        "surface_type": "TTTT", "in_race": "true", "current_lap": "1", "car_category": "GR1",
    }]

    assert _resolve_capture_width(default, rows, {}) is default


def _width_rec(slug, base, width_m, width_source, boundary_source):
    return {
        "slug": slug, "name": slug, "ddm_id": None, "hex_id": None, "base": base,
        "is_reverse": False, "is_oval": False, "length_m": 3000.0,
        "num_corners_expected": 6,
        "affine": {"scale": 1.0, "rot_deg": 0.0, "tx": 0.0, "tz": 0.0, "source": "capture"},
        "corners": [], "sectors": [],
        "boundary": {"source": boundary_source},
        "width": {"width_m": width_m, "min_m": None, "max_m": None, "source": width_source,
                  "n_total": None, "n_kept": None, "profile": None},
        "confidence": {"coordinate_usability": "generated-coarse",
                       "physical_boundary_fidelity": "derived", "join": "exact"},
        "provenance": {"affine": "capture", "corners": "kinematic",
                       "boundary": boundary_source, "width": width_source},
    }


def test_base_width_pass_stamps_best_evidence_across_all_layouts():
    """A base's width is one value shared by every layout: the best-evidence measurement
    (VictoryDash > telemetry width-lap > svg-stroke) is stamped on all layouts of the base."""
    records = {
        "sardegna-road-a": _width_rec("sardegna-road-a", 20, 10.41, "victorydash", "victorydash"),
        "sardegna-road-b": _width_rec("sardegna-road-b", 20, 17.21, "telemetry_width_lap", "outline"),
        "sardegna-road-c": _width_rec("sardegna-road-c", 20, 27.47, "svg_stroke", "outline"),
    }

    _apply_base_width_pass(records)

    for slug in records:
        assert records[slug]["width"]["width_m"] == 10.41
        assert records[slug]["width"]["source"] == "victorydash"
        assert records[slug]["provenance"]["width"] == "victorydash"
    # road-a keeps measured (vd boundary + vd width); the outline-boundary layouts are derived
    assert records["sardegna-road-a"]["confidence"]["physical_boundary_fidelity"] == "measured"
    assert records["sardegna-road-b"]["confidence"]["physical_boundary_fidelity"] == "derived"
    # copies, not a shared alias
    assert records["sardegna-road-a"]["width"] is not records["sardegna-road-b"]["width"]


def test_base_width_pass_venue_unifies_across_bases():
    """Sardegna Windmills (its own base) shares the Sardegna Road width: the venue prefix
    unions all sardegna-* layouts into one width group across distinct bases."""
    records = {
        "sardegna-road-a": _width_rec("sardegna-road-a", 26, 11.25, "victorydash", "victorydash"),
        "sardegna-windmills": _width_rec("sardegna-windmills", 27, 9.67, "svg_stroke", "outline"),
    }
    _apply_base_width_pass(records)
    assert records["sardegna-windmills"]["width"]["width_m"] == 11.25
    assert records["sardegna-windmills"]["width"]["source"] == "victorydash"
    assert records["sardegna-windmills"]["provenance"]["width"] == "victorydash"
    # windmills has no measured boundary of its own -> still 'derived', not falsely 'measured'
    assert records["sardegna-windmills"]["confidence"]["physical_boundary_fidelity"] == "derived"


def test_base_width_pass_venue_pulls_in_baseless_member():
    """A venue layout with base=None (unreconciled, e.g. nurburgring-endurance-2) is still
    caught by the venue prefix and gets the venue width, though base grouping would skip it."""
    records = {
        "nurburgring-gp": _width_rec("nurburgring-gp", 24, 12.09, "victorydash", "victorydash"),
        "nurburgring-endurance-2": _width_rec(
            "nurburgring-endurance-2", None, None, "svg_stroke", "outline"),
    }
    _apply_base_width_pass(records)
    assert records["nurburgring-endurance-2"]["width"]["width_m"] == 12.09
    assert records["nurburgring-endurance-2"]["width"]["source"] == "victorydash"


def test_base_width_pass_tie_break_first_seen_wins():
    """Equal-rank widths (strict >): the first-seen layout's value wins, shared to both."""
    records = {
        "x-a": _width_rec("x-a", 30, 12.0, "svg_stroke", "outline"),
        "x-b": _width_rec("x-b", 30, 9.0, "svg_stroke", "outline"),
    }
    _apply_base_width_pass(records)
    assert records["x-a"]["width"]["width_m"] == 12.0
    assert records["x-b"]["width"]["width_m"] == 12.0  # first-seen (x-a) wins the tie


def test_base_width_pass_fidelity_none_when_boundary_and_width_both_non_measured():
    """A layout whose own boundary is neither outline nor victorydash, sharing a
    telemetry_width_lap width, is 'none' fidelity (gate: not measured, not derived)."""
    records = {
        "y-a": _width_rec("y-a", 40, 13.0, "telemetry_width_lap", "capture"),
        "y-b": _width_rec("y-b", 40, 99.0, "svg_stroke", "capture"),
    }
    _apply_base_width_pass(records)
    # best evidence is telemetry_width_lap; y-a boundary 'capture' + width telemetry -> none
    assert records["y-a"]["width"]["source"] == "telemetry_width_lap"
    assert records["y-a"]["confidence"]["physical_boundary_fidelity"] == "none"


def test_base_width_pass_deepcopies_nested_profile():
    """Stamped width dicts must not alias a nested mutable `profile` across layouts."""
    a = _width_rec("z-a", 50, 10.0, "svg_stroke", "outline")
    a["width"]["profile"] = [1.0, 2.0]
    records = {"z-a": a, "z-b": _width_rec("z-b", 50, 9.0, "svg_stroke", "outline")}
    _apply_base_width_pass(records)
    assert records["z-a"]["width"]["profile"] is not records["z-b"]["width"]["profile"]


def test_base_width_pass_ignores_baseless_records():
    rec = _width_rec("solo", None, 8.0, "svg_stroke", "outline")
    records = {"solo": rec}
    _apply_base_width_pass(records)
    assert records["solo"]["width"]["width_m"] == 8.0  # untouched


def test_log_vd_surface_crosscheck_does_not_raise(capsys):
    rows = _synthetic_width_lap_rows()
    vd_width = {"width_m": 12.0, "source": "victorydash"}
    _log_vd_surface_crosscheck("sardegna-road-a", rows, vd_width)
    out = capsys.readouterr().out
    assert "sardegna-road-a" in out


def test_build_track_capture_stores_driven_line_cl_without_upgrading_fidelity():
    import rkb.sources as S
    from rkb.capture import centerline_from_capture

    kb = S.load_kb("tests/fixtures/kb_hsr")
    ref = TrackRef(
        slug="high-speed-ring-2", name="High Speed Ring Reverse", ddm_id=1069,
        num_corners=6, is_reverse=True, join="exact",
    )
    rec = build_track(ref, kb["high-speed-ring-2"], capture_csv="tests/fixtures/hsr_lap.csv")

    b = rec["boundary"]
    assert b["cl"] == centerline_from_capture("tests/fixtures/hsr_lap.csv")
    assert len(b["outline"]) > 0  # the SVG outline is still the boundary shape
    assert b["source"] == "outline"
    # provenance says the cl is a driven line, not a surveyed centreline
    assert rec["provenance"]["boundary"] == "outline+driven_line"
    assert rec["confidence"]["physical_boundary_fidelity"] == "derived"
    validate_record(rec)
