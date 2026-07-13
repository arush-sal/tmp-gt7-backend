import json
import math

from rkb.build import _json_safe


def _has_nonfinite(obj) -> bool:
    """Recursively report whether any float anywhere in obj is non-finite."""
    if isinstance(obj, float):
        return not math.isfinite(obj)
    if isinstance(obj, dict):
        return any(_has_nonfinite(v) for v in obj.values())
    if isinstance(obj, (list, tuple)):
        return any(_has_nonfinite(v) for v in obj)
    return False


def _nested_nan_record() -> dict:
    """A record shaped like a needs-capture track: NaN flows from affine.tx/tz
    through px_to_world into apex_world, boundary.outline, width_m, label_world."""
    return {
        "slug": "eiger-nordwand",
        "affine": {"scale": 0.6, "tx": math.nan, "tz": math.nan, "rms_m": 0.0},
        "corners": [
            {"id": 1, "apex_world": [math.nan, math.nan]},
            {"id": 2, "apex_world": [12.3, 45.6]},
        ],
        "boundary": {"source": "outline", "outline": [[math.nan, math.nan], [1.0, 2.0]]},
        "width": {"width_m": math.nan, "source": "svg_stroke"},
        "sectors": [{"label": "S1", "label_world": [math.nan, float("inf")]}],
        "confidence": {"coordinate_usability": "needs-capture"},
    }


def test_json_safe_sanitizes_all_nested_nonfinite():
    rec = _nested_nan_record()
    assert _has_nonfinite(rec)  # sanity: fixture really does contain non-finite floats

    safe = _json_safe(rec)

    # (a) no non-finite float survives anywhere in the record
    assert not _has_nonfinite(safe)
    # spot-check the previously-poisoned nested fields became None
    assert safe["affine"]["tx"] is None
    assert safe["affine"]["tz"] is None
    assert safe["corners"][0]["apex_world"] == [None, None]
    assert safe["boundary"]["outline"][0] == [None, None]
    assert safe["width"]["width_m"] is None
    assert safe["sectors"][0]["label_world"] == [None, None]
    # finite values are left intact
    assert safe["affine"]["scale"] == 0.6
    assert safe["affine"]["rms_m"] == 0.0
    assert safe["corners"][1]["apex_world"] == [12.3, 45.6]
    assert safe["boundary"]["outline"][1] == [1.0, 2.0]


def test_json_safe_output_serializes_under_strict_json():
    safe = _json_safe(_nested_nan_record())
    # (b) strict serialization (as Go's encoding/json requires) must not raise
    json.dumps(safe, allow_nan=False)
