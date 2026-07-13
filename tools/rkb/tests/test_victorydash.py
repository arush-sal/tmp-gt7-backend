import math
import os
import shutil

import pytest

from rkb.sources import load_kb
from rkb.victorydash import boundary_from_vd, corners_from_vd, width_from_vd, affine_from_vd

VD_DIR = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "..",
    "supporting-data", "victorydash", "interlagos-time-trial-a-tapai",
)
KB_DIR = "/home/salil/go/src/github.com/arush-sal/gt7/kb/track-layouts-svg"

skip_if_missing = pytest.mark.skipif(
    not os.path.exists(VD_DIR) or shutil.which("go") is None,
    reason="VictoryDash interlagos fixture or vd2csv (go) not available",
)


@skip_if_missing
def test_boundary_from_vd_returns_true_edges():
    b = boundary_from_vd(VD_DIR)
    assert b["source"] == "victorydash"
    assert len(b["left"]) > 1000
    assert len(b["right"]) > 1000


@skip_if_missing
def test_width_from_vd_is_a_real_track_width():
    w = width_from_vd(VD_DIR)
    assert w["source"] == "victorydash"
    assert 5 < w["width_m"] < 25


@skip_if_missing
def test_corners_from_vd_detects_most_of_interlagos_turns():
    corners = corners_from_vd(VD_DIR)
    assert len(corners) >= 8
    for c in corners:
        assert c["source"] == "kinematic"
        assert c["apex_world"] is not None
        assert c["radius_m"] is not None


@skip_if_missing
def test_affine_from_vd_fits_against_kb_interlagos_outline():
    kb = load_kb(KB_DIR)
    outline_px = kb["interlagos"]["outline_px"]
    aff = affine_from_vd(VD_DIR, outline_px)
    assert aff.source == "victorydash"
    assert aff.scale > 0
    assert math.isfinite(aff.rms_m)
