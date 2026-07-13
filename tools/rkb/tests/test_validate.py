from rkb.validate import leave_one_out, report


def test_report_counts_tiers_and_backlog():
    recs = [
        {"slug": "a", "confidence": {"coordinate_usability": "generated-reliable"},
         "num_corners_expected": 6, "corners": [1, 2, 3, 4, 5, 6]},
        {"slug": "b", "confidence": {"coordinate_usability": "needs-capture"},
         "num_corners_expected": 10, "corners": []},
    ]
    rep = report(recs)
    assert rep["tiers"]["generated-reliable"] == 1
    assert rep["tiers"]["needs-capture"] == 1
    assert rep["needs_capture_backlog"] == ["b"]
    assert rep["total"] == 2


def test_leave_one_out_reports_predicted_vs_fitted_scale_error():
    fitted = [
        {"slug": "hsr", "fitted_scale": 0.6, "perimeter_px": 1200.0, "length_m": 2000.0},
    ]
    out = leave_one_out(fitted)
    assert out[0]["slug"] == "hsr"
    assert out[0]["predicted_scale"] == 0.6
    assert out[0]["fitted_scale"] == 0.6
    assert out[0]["scale_err_pct"] == 0.0
