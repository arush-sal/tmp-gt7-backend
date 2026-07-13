import csv

from rkb.build import build_track
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
