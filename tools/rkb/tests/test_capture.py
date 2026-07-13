import math
import os
import shutil

import pytest

from rkb.affine import Affine
from rkb.capture import affine_from_capture, corners_from_capture, decode_capture
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
