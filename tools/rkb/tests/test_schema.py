import pytest
from rkb.schema import validate_record


def _min():
    return {
        "slug": "x",
        "name": "X",
        "affine": {"scale": 0.6, "tx": 0, "tz": 0, "source": "bbox"},
        "corners": [],
        "sectors": [],
        "boundary": {"source": "outline"},
        "width": {"width_m": 10.0},
        "confidence": {
            "coordinate_usability": "reliable",
            "physical_boundary_fidelity": "derived",
            "join": "exact",
        },
        "provenance": {},
    }


def test_validate_accepts_minimal_record():
    validate_record(_min())  # no raise


def test_validate_rejects_bad_coordinate_usability():
    d = _min()
    d["confidence"]["coordinate_usability"] = "great"
    with pytest.raises(ValueError):
        validate_record(d)


def test_validate_rejects_bad_physical_boundary_fidelity():
    d = _min()
    d["confidence"]["physical_boundary_fidelity"] = "perfect"
    with pytest.raises(ValueError):
        validate_record(d)


def test_validate_rejects_bad_join():
    d = _min()
    d["confidence"]["join"] = "maybe"
    with pytest.raises(ValueError):
        validate_record(d)


def test_validate_rejects_missing_required_keys():
    d = _min()
    del d["slug"]
    with pytest.raises(ValueError, match="missing keys"):
        validate_record(d)


def test_validate_rejects_missing_confidence_key():
    d = _min()
    del d["confidence"]
    with pytest.raises(ValueError, match="missing keys"):
        validate_record(d)


def test_validate_accepts_all_coordinate_usability_enums():
    for val in ["reliable", "coarse", "needs-capture"]:
        d = _min()
        d["confidence"]["coordinate_usability"] = val
        validate_record(d)  # no raise


def test_validate_accepts_all_fidelity_enums():
    for val in ["measured", "derived", "none"]:
        d = _min()
        d["confidence"]["physical_boundary_fidelity"] = val
        validate_record(d)  # no raise


def test_validate_accepts_all_join_enums():
    for val in ["exact", "fuzzy", "ambiguous"]:
        d = _min()
        d["confidence"]["join"] = val
        validate_record(d)  # no raise
