import math
import os
import shutil

import pytest

from rkb.sources import load_kb
from rkb.victorydash import boundary_from_vd, corners_from_vd, width_from_vd, affine_from_vd, decode_lap

VD_DIR = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "..",
    "supporting-data", "victorydash", "interlagos-time-trial-a-tapai",
)
KB_DIR = "/home/salil/go/src/github.com/arush-sal/gt7/kb/track-layouts-svg"

skip_if_missing = pytest.mark.skipif(
    not os.path.exists(VD_DIR) or shutil.which("go") is None,
    reason="VictoryDash interlagos fixture or vd2csv (go) not available",
)

# Suzuka is a figure-8 (self-crossing track): nearest-neighbor left/right
# boundary pairing can jump to the OTHER side of the track where it crosses
# itself, producing implausible sub-metre cross-sections if not guarded
# against (see rkb.width.measure).
SUZUKA_VD_DIR = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "..",
    "supporting-data", "victorydash", "week-29-daily-c-ryun",
)
skip_if_suzuka_missing = pytest.mark.skipif(
    not os.path.exists(SUZUKA_VD_DIR),
    reason="VictoryDash suzuka fixture not available",
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


def test_decode_lap_default_output_stays_out_of_the_read_only_vd_dir(tmp_path, monkeypatch):
    # vd_dir under supporting-data/victorydash/<slug>/ is read-only input data;
    # decode_lap must not write telemetry.csv into it by default.
    vd_dir = tmp_path / "some-vd-slug"
    vd_dir.mkdir()
    (vd_dir / "telemetry.json").write_text("{}")

    recorded = {}

    def fake_run(cmd, cwd=None, check=None):
        recorded["cmd"] = cmd

    monkeypatch.setattr("rkb.victorydash.subprocess.run", fake_run)

    out = decode_lap(str(vd_dir))

    assert str(vd_dir) not in out
    assert "supporting-data" not in out
    assert os.path.join("out", "vd-csv", "some-vd-slug.csv") in out
    assert os.path.isdir(os.path.dirname(out))
    # the produced CSV path passed through to vd2csv matches what decode_lap returned
    assert recorded["cmd"][-1] == out


@skip_if_suzuka_missing
def test_width_from_vd_rejects_crossover_mispairing_on_suzuka():
    w = width_from_vd(SUZUKA_VD_DIR)
    assert w["source"] == "victorydash"  # only 14/5799 mis-pairings -- well above the fallback floor
    assert w["min_m"] >= 3.0
    assert w["max_m"] <= 40.0


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
    # The world<->px transform is a KNOWN uniform scale+translation, no rotation.
    assert aff.rot_deg == 0
    assert aff.scale > 0
    # rms_m is a real residual (SVG-vs-truth agreement); it may be tens of metres
    # because the SVG is a stylized drawing, so only assert it's a finite number.
    assert math.isfinite(aff.rms_m)
    # Scale is plausible for a world(m)->px SVG fit.
    assert 0.1 < aff.scale < 5
    # Roundtrip sanity: projecting the cl bbox corners lands them within the
    # outline px bbox (the fit is a bbox-align, so this must hold).
    import json
    import os
    with open(os.path.join(VD_DIR, "track.json")) as f:
        cl = json.load(f)["cl"]
    xs = [p[0] for p in cl]
    zs = [p[1] for p in cl]
    minx, maxx, minz, maxz = min(xs), max(xs), min(zs), max(zs)
    pxs = [p[0] for p in outline_px]
    pys = [p[1] for p in outline_px]
    pminx, pmaxx, pminy, pmaxy = min(pxs), max(pxs), min(pys), max(pys)
    # fit_bbox uses one averaged scale for both axes (it permits <=5% anisotropy),
    # so a far corner can overshoot its axis span by up to a few percent. Allow that.
    ex = 0.05 * (pmaxx - pminx)
    ey = 0.05 * (pmaxy - pminy)
    for wx, wz in [(minx, minz), (minx, maxz), (maxx, minz), (maxx, maxz)]:
        px, py = aff.world_to_px(wx, wz)
        assert pminx - ex <= px <= pmaxx + ex
        assert pminy - ey <= py <= pmaxy + ey
