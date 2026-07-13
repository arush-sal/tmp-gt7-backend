import json
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
        "width": {"width_m": 10.0, "source": "svg_stroke"},
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


def test_db_is_lossless_via_record_json(tmp_path):
    """The DB is the source of truth: tracks.record_json must round-trip the FULL record."""
    rec = _rec()
    p = tmp_path / "r.db"
    build_db([rec], str(p))
    con = sqlite3.connect(p)
    stored = con.execute("select record_json from tracks where slug='x'").fetchone()[0]
    assert json.loads(stored) == rec  # nothing dropped


def test_db_enriched_corner_and_affine_columns(tmp_path):
    """Rich kinematic corner fields + affine diagnostics are queryable, not just in the blob."""
    p = tmp_path / "r.db"
    build_db([_rec()], str(p))
    con = sqlite3.connect(p)
    row = con.execute(
        "select apex_speed_kmh, lat_accel_g, gear_at_apex, heading_change_deg, phases_json "
        "from corners where slug='x'"
    ).fetchone()
    assert row[0] == 120.5 and row[1] == 1.9 and row[2] == 3 and row[3] == 88.0
    assert json.loads(row[4])["exit"] == [5.0, 6.0]
    aff = con.execute(
        "select affine_sf_anchor_err_m, affine_world_bbox, prov_corners from tracks where slug='x'"
    ).fetchone()
    assert aff[0] == 7.5
    assert json.loads(aff[1]) == [-1.0, -2.0, 3.0, 4.0]
    assert aff[2] == "kinematic"
