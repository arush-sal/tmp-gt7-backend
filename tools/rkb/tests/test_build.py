import csv
import math

from rkb.build import _log_vd_surface_crosscheck, _resolve_capture_width, build_track
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


def test_log_vd_surface_crosscheck_does_not_raise(capsys):
    rows = _synthetic_width_lap_rows()
    vd_width = {"width_m": 12.0, "source": "victorydash"}
    _log_vd_surface_crosscheck("sardegna-road-a", rows, vd_width)
    out = capsys.readouterr().out
    assert "sardegna-road-a" in out
