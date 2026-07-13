from rkb.reverse import derive_reverse
from rkb.schema import validate_record


def _forward_record():
    return {
        "slug": "example-fwd",
        "name": "Example Forward",
        "ddm_id": 100,
        "hex_id": "0x64",
        "base": "example",
        "is_reverse": False,
        "is_oval": False,
        "length_m": 3000.0,
        "num_corners_expected": 3,
        "affine": {"scale": 0.6, "rot_deg": 0.0, "tx": 1.0, "tz": 2.0, "source": "bbox"},
        "corners": [
            {
                "turn": 1,
                "apex_world": [10.0, 20.0],
                "radius_m": 50.0,
                "direction": "left",
                "heading_change_deg": 90.0,
                "type": "medium",
                "apex_speed_kmh": 120.0,
                "lat_accel_g": 1.1,
                "gear_at_apex": 4,
                "phases": {
                    "braking_zone": [9.0, 19.0],
                    "turn_in": [9.5, 19.5],
                    "apex": [10.0, 20.0],
                    "exit": [10.5, 20.5],
                },
                "source": "kinematic",
            },
            {
                "turn": 2,
                "apex_world": [30.0, 40.0],
                "radius_m": 80.0,
                "direction": "right",
                "heading_change_deg": -60.0,
                "type": "sweeper",
                "apex_speed_kmh": 150.0,
                "lat_accel_g": 0.9,
                "gear_at_apex": 5,
                "phases": {
                    "braking_zone": None,
                    "turn_in": [29.0, 39.0],
                    "apex": [30.0, 40.0],
                    "exit": [31.0, 41.0],
                },
                "source": "kinematic",
            },
            {
                "turn": 3,
                "apex_world": [50.0, 60.0],
                "radius_m": 40.0,
                "direction": "left",
                "heading_change_deg": 100.0,
                "type": "tight",
                "apex_speed_kmh": 90.0,
                "lat_accel_g": 1.3,
                "gear_at_apex": 3,
                "phases": {
                    "braking_zone": [49.0, 59.0],
                    "turn_in": [49.5, 59.5],
                    "apex": [50.0, 60.0],
                    "exit": [50.5, 60.5],
                },
                "source": "kinematic",
            },
        ],
        "sectors": [
            {"sector": "S1", "turns": [1, 2], "label_world": [15.0, 25.0]},
            {"sector": "S2", "turns": [2, 3], "label_world": [35.0, 45.0]},
        ],
        "boundary": {"source": "outline", "outline": [[0.0, 0.0], [100.0, 100.0]]},
        "width": {"width_m": 12.0, "source": "svg_stroke"},
        "confidence": {
            "coordinate_usability": "generated-reliable",
            "physical_boundary_fidelity": "derived",
            "join": "exact",
        },
        "provenance": {
            "affine": "bbox_from_driven_trace",
            "corners": "kinematic",
            "boundary": "outline",
            "width": "svg_stroke",
        },
    }


def _reverse_ident():
    return {
        "slug": "example-rev",
        "name": "Example Reverse",
        "ddm_id": 101,
        "hex_id": "0x65",
        "base": "example",
        "length_m": 3000.0,
        "num_corners_expected": 3,
        "is_reverse": True,
        "sectors": None,
        "join": "exact",
    }


def test_derive_reverse_reverses_and_renumbers_corners():
    rec = derive_reverse(_forward_record(), _reverse_ident())
    corners = rec["corners"]
    assert [c["turn"] for c in corners] == [1, 2, 3]

    # old T3 (left, apex 50,60) -> new T1, direction flipped to right
    # heading_change_deg stays an unsigned magnitude (kinematic convention) --
    # direction alone carries sense, so it is NOT negated on flip.
    assert corners[0]["apex_world"] == [50.0, 60.0]
    assert corners[0]["direction"] == "right"
    assert corners[0]["heading_change_deg"] == 100.0

    # old T2 (right) -> new T2, direction flipped to left
    assert corners[1]["apex_world"] == [30.0, 40.0]
    assert corners[1]["direction"] == "left"
    assert corners[1]["heading_change_deg"] == 60.0

    # old T1 (left, apex 10,20) -> new T3, direction flipped to right
    assert corners[2]["apex_world"] == [10.0, 20.0]
    assert corners[2]["direction"] == "right"
    assert corners[2]["heading_change_deg"] == 90.0


def test_derive_reverse_drops_direction_specific_corner_fields():
    rec = derive_reverse(_forward_record(), _reverse_ident())
    for c in rec["corners"]:
        assert c.get("phases") is None
        assert "apex_speed_kmh" not in c
        assert "lat_accel_g" not in c
        assert "gear_at_apex" not in c
        assert c["source"] == "derived-reverse"
        # physical shape info retained
        assert "radius_m" in c
        assert "type" in c


def test_derive_reverse_shares_affine_boundary_width_with_forward():
    forward = _forward_record()
    rec = derive_reverse(forward, _reverse_ident())
    assert rec["affine"] == forward["affine"]
    assert rec["boundary"] == forward["boundary"]
    assert rec["width"] == forward["width"]


def test_derive_reverse_identity_from_reverse_ident():
    rec = derive_reverse(_forward_record(), _reverse_ident())
    assert rec["slug"] == "example-rev"
    assert rec["name"] == "Example Reverse"
    assert rec["ddm_id"] == 101
    assert rec["hex_id"] == "0x65"
    assert rec["is_reverse"] is True


def test_derive_reverse_uses_own_sectors_when_given():
    ident = _reverse_ident()
    ident["sectors"] = [{"sector": "S1", "turns": [1, 2], "label_world": [1.0, 1.0]}]
    rec = derive_reverse(_forward_record(), ident)
    assert rec["sectors"] == ident["sectors"]


def test_derive_reverse_falls_back_to_reversed_forward_sectors():
    rec = derive_reverse(_forward_record(), _reverse_ident())
    assert [s["sector"] for s in rec["sectors"]] == ["S1", "S2"]
    # reversed order of forward's sector list, renumbered; turns remapped too:
    # for N=3 corners, forward turn t -> reverse turn (N+1-t) = (4-t).
    # old S2 (turns [2,3]) -> new S1, turns become sorted([4-2, 4-3]) = [1, 2]
    assert rec["sectors"][0]["turns"] == [1, 2]
    assert rec["sectors"][0]["label_world"] == [35.0, 45.0]
    # old S1 (turns [1,2]) -> new S2, turns become sorted([4-1, 4-2]) = [2, 3]
    assert rec["sectors"][1]["turns"] == [2, 3]
    assert rec["sectors"][1]["label_world"] == [15.0, 25.0]


def test_derive_reverse_confidence_and_provenance():
    forward = _forward_record()
    rec = derive_reverse(forward, _reverse_ident())
    assert rec["confidence"]["coordinate_usability"] == forward["confidence"]["coordinate_usability"]
    assert rec["confidence"]["physical_boundary_fidelity"] == forward["confidence"]["physical_boundary_fidelity"]
    assert rec["confidence"]["join"] == "exact"
    assert rec["provenance"]["affine"] == forward["provenance"]["affine"]
    assert rec["provenance"]["corners"] == "derived-reverse"
    assert rec["provenance"]["boundary"] == forward["provenance"]["boundary"]
    assert rec["provenance"]["width"] == forward["provenance"]["width"]


def test_derive_reverse_passes_schema_validation():
    rec = derive_reverse(_forward_record(), _reverse_ident())
    validate_record(rec)  # no raise
