import math
import os
import shutil

import pytest

from rkb.affine import Affine
from rkb.capture import (
    affine_from_capture,
    assign_segments_to_slugs,
    corners_from_capture,
    decode_capture,
    resolve_forward_reverse,
    segment_winding_sign,
    split_capture_by_jumps,
)
from rkb.sources import load_kb

_RAW_DIR = os.path.join(
    os.path.dirname(__file__), "..", "..", "..", "..",
    "supporting-data", "raw-packet-captures",
)
GVS_PCAP = os.path.join(
    _RAW_DIR,
    "grand-valley-south-reverse-4laps-1stRight-2ndLeft_with_pit_and_track_width-3rdLeft_with_pit_4thCentre.pcapng",
)
BH_PCAP = os.path.join(_RAW_DIR, "brand-hatch-indy-3laps-3rd-clean.pcapng")
KB_DIR = "/home/salil/go/src/github.com/arush-sal/gt7/kb/track-layouts-svg"

skip_if_missing = pytest.mark.skipif(
    not os.path.exists(GVS_PCAP) or shutil.which("go") is None,
    reason="grand-valley-south-2 capture or go toolchain not available",
)


def test_decode_capture_default_output_stays_out_of_supporting_data(tmp_path, monkeypatch):
    # supporting-data/raw-packet-captures/ is read-only input data; decode_capture
    # must not write its CSV there by default.
    pcap = tmp_path / "some-capture.pcapng"
    pcap.write_bytes(b"")

    recorded = {}

    def fake_run(cmd, cwd=None, check=None):
        recorded["cmd"] = cmd
        recorded["cwd"] = cwd

    monkeypatch.setattr("rkb.capture.subprocess.run", fake_run)

    out = decode_capture(str(pcap))

    assert "supporting-data" not in out
    assert out.endswith("some-capture.csv")
    assert recorded["cmd"][0] == "go"
    assert recorded["cmd"][1] == "run"
    assert recorded["cmd"][2] == "./tools/rkb/pcap2csv"


def test_affine_from_capture_bbox_fits_outline(tmp_path):
    csv_path = tmp_path / "trace.csv"
    csv_path.write_text(
        "pos_x,pos_z,in_race\n"
        "0,0,true\n"
        "10,0,true\n"
        "10,10,true\n"
        "0,10,true\n"
        "0,0,false\n"  # not-in-race row must be excluded from the bbox
        "999,999,false\n"
    )
    outline_px = [(0, 0), (100, 0), (100, 100), (0, 100)]

    aff = affine_from_capture(str(csv_path), outline_px)

    assert isinstance(aff, Affine)
    assert aff.source == "capture"
    assert not math.isnan(aff.tx)
    assert aff.scale == pytest.approx(10.0)


@skip_if_missing
def test_corners_from_capture_grand_valley_south_2():
    csv_path = decode_capture(GVS_PCAP)
    corners = corners_from_capture(csv_path)
    assert len(corners) > 0
    assert corners[0]["source"] == "kinematic"


@skip_if_missing
def test_affine_from_capture_grand_valley_south_2_resolves_offset():
    kb = load_kb(KB_DIR)
    csv_path = decode_capture(GVS_PCAP)
    aff = affine_from_capture(csv_path, kb["grand-valley-south-2"]["outline_px"])
    assert aff.source == "capture"
    assert not math.isnan(aff.tx)
    assert not math.isnan(aff.tz)


def _row(x, z):
    return {"pos_x": str(x), "pos_z": str(z), "in_race": "true"}


def _square_loop(cx, cz, r=5.0, n=2500, clockwise=False):
    """A synthetic driven square-ish loop around (cx, cz), n points, closed."""
    import math as _m

    pts = []
    for i in range(n):
        t = 2 * _m.pi * i / n
        if clockwise:
            t = -t
        pts.append(_row(cx + r * _m.cos(t), cz + r * _m.sin(t)))
    return pts


def test_split_capture_by_jumps_separates_two_layouts_and_drops_stub_segments():
    seg_a = _square_loop(0, 0, n=2500)
    teleport_stub = [_row(9999, 9999)]  # 1-point teleport artifact, well under min_len
    seg_b = _square_loop(500, 500, n=2600)
    rows = seg_a + teleport_stub + seg_b

    segments = split_capture_by_jumps(rows, jump_m=100.0, min_len=2000)

    assert len(segments) == 2
    assert len(segments[0]) == len(seg_a)
    assert len(segments[1]) == len(seg_b)


def test_split_capture_by_jumps_ignores_not_in_race_rows():
    rows = [{"pos_x": "0", "pos_z": "0", "in_race": "false"}] + _square_loop(0, 0, n=2100)
    segments = split_capture_by_jumps(rows, jump_m=100.0, min_len=2000)
    assert len(segments) == 1
    assert all(r["in_race"] == "true" for r in segments[0])


def test_segment_winding_sign_opposite_for_opposite_direction():
    cw = _square_loop(0, 0, n=200, clockwise=True)
    ccw = _square_loop(0, 0, n=200, clockwise=False)
    assert segment_winding_sign(cw) == -segment_winding_sign(ccw)


def test_assign_segments_to_slugs_matches_by_nearest_center_and_pairs_shared_centers():
    seg_a_fwd = _square_loop(0, 0, n=2200, clockwise=False)
    seg_a_rev = _square_loop(0, 0, n=2300, clockwise=True)
    seg_b = _square_loop(500, 500, n=2400)
    segments = [seg_a_fwd, seg_a_rev, seg_b]

    slug_bboxes = {
        "route-a": (0.0, 0.0),
        "route-a2": (0.0, 0.0),
        "route-b": (500.0, 500.0),
    }
    assigned = assign_segments_to_slugs(segments, slug_bboxes)

    assert assigned["route-b"] == [seg_b]
    # route-a / route-a2 share a center -> both claim the SAME 2-segment list
    assert assigned["route-a"] == assigned["route-a2"]
    assert len(assigned["route-a"]) == 2
    assert seg_a_fwd in assigned["route-a"]
    assert seg_a_rev in assigned["route-a"]


def test_resolve_forward_reverse_splits_pair_by_winding():
    seg_a_fwd = _square_loop(0, 0, n=2200, clockwise=False)
    seg_a_rev = _square_loop(0, 0, n=2300, clockwise=True)
    seg_b = _square_loop(500, 500, n=2400)
    forward_sign = segment_winding_sign(seg_a_fwd)

    assigned = {
        "route-a": [seg_a_fwd, seg_a_rev],
        "route-a2": [seg_a_fwd, seg_a_rev],
        "route-b": [seg_b],
    }
    resolved = resolve_forward_reverse(
        assigned,
        {"route-a": forward_sign, "route-a2": -forward_sign, "route-b": segment_winding_sign(seg_b)},
    )

    assert resolved["route-a"] == seg_a_fwd
    assert resolved["route-a2"] == seg_a_rev
    assert resolved["route-b"] == seg_b
