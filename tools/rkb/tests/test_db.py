import sqlite3

from rkb.db import build_db


def _rec():
    return {
        "slug": "x",
        "name": "X",
        "ddm_id": 11,
        "hex_id": "a",
        "base": 20,
        "is_reverse": False,
        "is_oval": False,
        "length_m": 4345.0,
        "num_corners_expected": 6,
        "affine": {
            "scale": 0.6,
            "rot_deg": 0.0,
            "tx": 1,
            "tz": 2,
            "source": "bbox_from_driven_trace",
            "rms_m": None,
            "sf_anchor_err_m": 7.5,
            "scale_sanity_pct": 0.9,
            "world_bbox": [-1.0, -2.0, 3.0, 4.0],
            "svg_bbox": [0.0, 0.0, 100.0, 100.0],
        },
        "corners": [
            {
                "turn": 1,
                "apex_world": [1.0, 2.0],
                "radius_m": 50.0,
                "direction": "left",
                "type": "tight",
                "source": "kinematic",
                "heading_change_deg": 88.0,
                "apex_speed_kmh": 120.5,
                "lat_accel_g": 1.9,
                "gear_at_apex": 3,
                "banked": False,
                "roll_deg_at_apex": 4.5,
                "length_m": 62.0,
                "phases": {"apex": [1.0, 2.0], "exit": [5.0, 6.0]},
            }
        ],
        "sectors": [{"sector": "S1", "turns": [1, 2], "label_world": [3.0, 4.0]}],
        "boundary": {"source": "outline", "outline": [[0.0, 0.0], [1.0, 1.0]]},
        "width": {"width_m": 10.0, "source": "svg_stroke"},
        "confidence": {
            "coordinate_usability": "generated-reliable",
            "physical_boundary_fidelity": "derived",
            "join": "exact",
        },
        "provenance": {
            "affine": "bbox_from_driven_trace",
            "corners": "kinematic",
            "boundary": "outline",
            "width": "svg_stroke",
        },
    }


def test_build_db_tables_and_rows(tmp_path):
    p = tmp_path / "r.db"
    build_db([_rec()], str(p))
    con = sqlite3.connect(p)
    assert con.execute("select count(*) from tracks").fetchone()[0] == 1
    assert con.execute("select count(*) from corners").fetchone()[0] == 1
    assert con.execute("select turn from corners where slug='x'").fetchone()[0] == 1


def test_db_enriched_corner_and_affine_columns(tmp_path):
    """Rich kinematic corner fields + affine diagnostics are queryable, not just in a blob."""
    p = tmp_path / "r.db"
    build_db([_rec()], str(p))
    con = sqlite3.connect(p)
    row = con.execute(
        "select apex_speed_kmh, lat_accel_g, gear_at_apex, corner_angle_deg "
        "from corners where slug='x'"
    ).fetchone()
    assert row[0] == 120.5 and row[1] == 1.9 and row[2] == 3 and row[3] == 88.0
    aff = con.execute(
        "select affine_sf_anchor_err_m, affine_world_bbox_min_x, affine_world_bbox_max_z, "
        "provenance_corners from tracks where slug='x'"
    ).fetchone()
    assert aff[0] == 7.5
    assert aff[1] == -1.0 and aff[2] == 4.0
    assert aff[3] == "kinematic"


def test_schema_has_no_record_json(tmp_path):
    p = tmp_path / "r.db"
    build_db([_rec()], str(p))
    con = sqlite3.connect(p)
    cols = [r[1] for r in con.execute("PRAGMA table_info(tracks)")]
    assert "record_json" not in cols
    # bbox expanded to scalar columns
    for c in ("affine_world_bbox_min_x", "affine_world_bbox_max_z",
              "affine_svg_bbox_min_x", "affine_svg_bbox_max_y"):
        assert c in cols
    assert "join_confidence" in cols and "provenance_affine" in cols


def test_corner_columns_renamed_and_added(tmp_path):
    p = tmp_path / "r.db"
    build_db([_rec()], str(p))
    con = sqlite3.connect(p)
    cols = [r[1] for r in con.execute("PRAGMA table_info(corners)")]
    for c in ("apex_x", "apex_z", "corner_angle_deg", "banking_deg_at_apex", "length_m"):
        assert c in cols
    assert "x" not in cols and "heading_change_deg" not in cols and "phases_json" not in cols


def test_corner_phases_table_and_derived_distance(tmp_path):
    p = tmp_path / "r.db"
    build_db([_rec()], str(p))
    con = sqlite3.connect(p)
    rows = con.execute(
        "select phase, world_x, world_z, phase_distance_from_apex "
        "from corner_phases where slug='x' order by phase").fetchall()
    phases = {r[0]: r for r in rows}
    assert "apex" in phases and "exit" in phases
    # apex phase distance from apex point is 0
    assert abs(phases["apex"][3] - 0.0) < 1e-9
    # exit distance = hypot(exit - apex)
    import math
    ax, az = phases["apex"][1], phases["apex"][2]
    ex, ez = phases["exit"][1], phases["exit"][2]
    assert abs(phases["exit"][3] - math.hypot(ex - ax, ez - az)) < 1e-9


def test_reference_times_table_empty(tmp_path):
    p = tmp_path / "r.db"
    build_db([_rec()], str(p))
    con = sqlite3.connect(p)
    cols = [r[1] for r in con.execute("PRAGMA table_info(reference_times)")]
    assert cols == ["slug", "sector", "car_id", "car_category",
                    "performance_point", "reference_time_ms"]
    assert con.execute("select count(*) from reference_times").fetchone()[0] == 0


def test_boundary_points_renamed(tmp_path):
    p = tmp_path / "r.db"
    build_db([_rec()], str(p))
    con = sqlite3.connect(p)
    cols = [r[1] for r in con.execute("PRAGMA table_info(boundary_points)")]
    assert "world_x" in cols and "world_z" in cols and "x" not in cols


def test_width_is_single_scalar_no_profile_or_range(tmp_path):
    """Width is a single constant width_m + source: no min/max range column and no
    per-distance width_profile table."""
    rec = _rec()
    rec["width"] = {"width_m": 10.0, "source": "victorydash"}
    p = tmp_path / "r.db"; build_db([rec], str(p))
    con = sqlite3.connect(p)
    cols = [r[1] for r in con.execute("PRAGMA table_info(tracks)")]
    assert "width_m" in cols and "width_source" in cols
    assert "width_min_m" not in cols and "width_max_m" not in cols
    assert "width_n_total" not in cols and "width_n_kept" not in cols
    assert con.execute("select width_m, width_source from tracks where slug='x'").fetchone() == (10.0, "victorydash")
    tables = [r[0] for r in con.execute("select name from sqlite_master where type='table'")]
    assert "width_profile" not in tables


def test_boundary_cl_side_stored(tmp_path):
    rec = _rec()
    rec["boundary"] = {"source": "victorydash", "left": [[0.0, 0.0]], "right": [[1.0, 1.0]],
                       "cl": [[0.5, 0.5], [0.6, 0.6]]}
    p = tmp_path / "r.db"; build_db([rec], str(p))
    con = sqlite3.connect(p)
    sides = dict(con.execute("select side, count(*) from boundary_points where slug='x' group by side"))
    assert sides.get("cl") == 2 and sides.get("left") == 1 and sides.get("right") == 1


def test_phase_distance_null_safe_on_null_apex(tmp_path):
    """A corner whose apex phase point is [None,None] must not crash; phase distance stored NULL."""
    rec = _rec()
    rec["corners"] = [{"turn": 1, "apex_world": [None, None], "source": "kinematic",
                       "phases": {"braking_zone": [5.0, 5.0], "apex": [None, None]}}]
    p = tmp_path / "r.db"; build_db([rec], str(p))  # must not raise
    con = sqlite3.connect(p)
    rows = con.execute(
        "select phase, phase_distance_from_apex from corner_phases where slug='x'").fetchall()
    assert rows == [("braking_zone", None)]  # incomplete apex point not stored; distance NULL
