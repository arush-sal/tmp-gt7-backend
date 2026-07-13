import csv

from rkb.index import write_index


def test_write_index(tmp_path):
    rec = {
        "slug": "x",
        "ddm_id": 11,
        "hex_id": "a",
        "name": "X",
        "base": 20,
        "is_reverse": False,
        "length_m": 4345.0,
        "num_corners_expected": 6,
        "width_m": 10.0,
        "affine": {"scale": 0.6},
        "confidence": {
            "coordinate_usability": "generated-reliable",
            "physical_boundary_fidelity": "derived",
            "join": "exact",
        },
    }
    p = tmp_path / "i.csv"
    write_index([rec], str(p))
    row = next(csv.DictReader(open(p)))
    assert row["slug"] == "x" and row["coordinate_usability"] == "generated-reliable"
    assert row["needs_capture"] == "False"
