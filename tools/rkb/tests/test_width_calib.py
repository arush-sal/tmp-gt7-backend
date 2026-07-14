import json

from rkb import width_calib


def _write_track_json(tmp_path, track):
    d = tmp_path / "vd"
    d.mkdir()
    (d / "track.json").write_text(json.dumps(track))
    return str(d)


def test_vd_width_median_of_aligned_lb_rb_dropping_outliers(tmp_path):
    # Uniform 12m-wide cross-sections plus one 80m outlier (self-crossing/mis-align
    # artifact) that must be dropped before taking the median -- same guard style as
    # rkb.width's victorydash measurement.
    track = {
        "cl": [[0.0, 0.0], [0.0, 10.0], [0.0, 20.0]],
        "lb": [[0.0, 0.0], [0.0, 10.0], [0.0, 20.0]],
        "rb": [[12.0, 0.0], [12.0, 10.0], [80.0, 20.0]],
    }
    vd_dir = _write_track_json(tmp_path, track)
    assert width_calib.vd_width(vd_dir) == 12.0


def test_build_calibration_correction_is_true_minus_raw(tmp_path, monkeypatch):
    track = {
        "cl": [[0.0, 0.0], [0.0, 10.0]],
        "lb": [[0.0, 0.0], [0.0, 10.0]],
        "rb": [[12.0, 0.0], [12.0, 10.0]],
    }
    vd_dir = _write_track_json(tmp_path, track)

    entry = width_calib.build_calibration(vd_dir, anchor_raw_width_m=10.0, car_category="GR1", anchor_slug="sardegna-road-a")

    assert entry["car_category"] == "GR1"
    assert entry["anchor_slug"] == "sardegna-road-a"
    assert entry["w_true_m"] == 12.0
    assert entry["w_raw_m"] == 10.0
    assert entry["correction_m"] == 2.0
    assert entry["confidence"] == "low"


def test_calibrated_width_applies_correction_when_car_matches_and_coverage_ok():
    raw = {
        "width_m": 10.0, "min_m": 8.0, "max_m": 12.0,
        "source": "telemetry_width_lap", "n_total": 100, "n_kept": 50,
        "profile": {"n_left": 25, "n_right": 25, "publishable_measured": False, "calibration_id": None},
    }
    table = {"GR1": {"correction_m": 1.5, "anchor_slug": "sardegna-road-a", "confidence": "low"}}

    out, publishable = width_calib.calibrated_width(raw, "GR1", table, min_n_kept=30)

    assert out["width_m"] == 11.5
    assert out["min_m"] == 9.5
    assert out["max_m"] == 13.5
    assert out["source"] == "telemetry_width_lap"
    assert out["n_total"] == 100 and out["n_kept"] == 50
    assert out["profile"] is None  # DB/schema/Go-safe shape: profile must stay a list-or-None
    assert publishable is True


def test_calibrated_width_leaves_raw_when_car_does_not_match_anchor():
    raw = {
        "width_m": 10.0, "min_m": 8.0, "max_m": 12.0,
        "source": "telemetry_width_lap", "n_total": 100, "n_kept": 50, "profile": {},
    }
    table = {"GR1": {"correction_m": 1.5, "anchor_slug": "sardegna-road-a", "confidence": "low"}}

    out, publishable = width_calib.calibrated_width(raw, "GRX", table, min_n_kept=30)

    assert out["width_m"] == 10.0  # uncorrected
    assert publishable is False


def test_calibrated_width_not_publishable_when_coverage_too_low():
    raw = {
        "width_m": 10.0, "min_m": None, "max_m": None,
        "source": "telemetry_width_lap", "n_total": 100, "n_kept": 5, "profile": {},
    }
    table = {"GR1": {"correction_m": 1.5, "anchor_slug": "sardegna-road-a", "confidence": "low"}}

    out, publishable = width_calib.calibrated_width(raw, "GR1", table, min_n_kept=30)

    assert out["width_m"] == 11.5  # still corrected -- car matches, just not "publishable"
    assert publishable is False


def test_calibrated_width_passthrough_when_width_m_is_none():
    raw = {
        "width_m": None, "min_m": None, "max_m": None,
        "source": "telemetry_width_lap", "n_total": 10, "n_kept": 0, "profile": {},
    }
    out, publishable = width_calib.calibrated_width(raw, "GR1", {"GR1": {"correction_m": 1.5}})
    assert out["width_m"] is None
    assert publishable is False


def test_select_width_lap_picks_lap_with_most_off_tarmac_events():
    rows = []
    for lap, off_count, total in [("0", 0, 400), ("1", 50, 500), ("2", 5, 400)]:
        for i in range(total):
            surface = "DTTT" if i < off_count else "TTTT"
            rows.append({"current_lap": lap, "surface_type": surface, "pos_x": "0.0", "pos_z": "0.0"})

    selected = width_calib.select_width_lap(rows, min_lap_rows=300)

    assert selected and all(r["current_lap"] == "1" for r in selected)
    assert len(selected) == 500


def test_select_width_lap_ignores_stub_laps_below_min_rows():
    rows = []
    # A tiny 10-row "lap" with a spuriously high off-tarmac fraction (a teleport/pit
    # stub, not a real lap) must not out-rank the real, longer width lap.
    for i in range(10):
        rows.append({"current_lap": "0", "surface_type": "DTTT", "pos_x": "0.0", "pos_z": "0.0"})
    for i in range(2000):
        surface = "DTTT" if i < 100 else "TTTT"
        rows.append({"current_lap": "1", "surface_type": surface, "pos_x": "0.0", "pos_z": "0.0"})

    selected = width_calib.select_width_lap(rows, min_lap_rows=1500)

    assert all(r["current_lap"] == "1" for r in selected)
