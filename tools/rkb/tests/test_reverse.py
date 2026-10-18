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


# ---- trust of the reversed apex order (Go rtps/rkb/detection.go trustedApexSources) ----

def _with_corner_source(src):
    fwd = _forward_record()
    for c in fwd["corners"]:
        c["source"] = src
    fwd["provenance"]["corners"] = src
    return fwd


def test_reverse_of_driven_lap_corners_is_trusted_derived_reverse():
    rev = derive_reverse(_with_corner_source("kinematic"), _reverse_ident())
    assert {c["source"] for c in rev["corners"]} == {"derived-reverse"}
    assert rev["provenance"]["corners"] == "derived-reverse"


def test_reverse_of_untrusted_corners_keeps_the_untrusted_source():
    # Stored labels / outline curvature follow SVG order, not travel: reversing them must not
    # mint "derived-reverse", which the detector trusts as the real direction of travel.
    for src in ("stored", "outline_curvature"):
        rev = derive_reverse(_with_corner_source(src), _reverse_ident())
        validate_record(rev)
        assert {c["source"] for c in rev["corners"]} == {src}, src
        assert rev["provenance"]["corners"] == src


# ---- measured, directed boundaries swap sides for the reverse travel direction ----

def test_reverse_swaps_and_reverses_measured_left_right_boundary():
    fwd = _forward_record()
    fwd["boundary"] = {"source": "victorydash",
                       "left": [[0.0, 0.0], [1.0, 0.0], [2.0, 0.0]],
                       "right": [[0.0, 9.0], [1.0, 9.0], [2.0, 9.0]],
                       "cl": [[0.0, 4.5], [10.0, 4.5], [5.0, 8.0]]}  # a (tiny) lap
    rev = derive_reverse(fwd, _reverse_ident())
    validate_record(rev)
    b = rev["boundary"]
    assert b["source"] == "victorydash"
    assert b["left"] == [[2.0, 9.0], [1.0, 9.0], [0.0, 9.0]]    # forward right, reverse travel order
    assert b["right"] == [[2.0, 0.0], [1.0, 0.0], [0.0, 0.0]]
    assert b["cl"] == [[0.0, 4.5], [5.0, 8.0], [10.0, 4.5]]  # same lap, other way, from start/finish
    assert fwd["boundary"]["left"] == [[0.0, 0.0], [1.0, 0.0], [2.0, 0.0]]  # parent untouched


def test_reverse_keeps_undirected_outline_boundary():
    fwd = _forward_record()
    rev = derive_reverse(fwd, _reverse_ident())
    assert rev["boundary"] == fwd["boundary"]


def test_reverse_phases_follow_the_source_contract():
    # db.py / Go setPhases: derived-reverse -> explicit null; stored/outline_curvature -> absent.
    rev = derive_reverse(_with_corner_source("kinematic"), _reverse_ident())
    assert all("phases" in c and c["phases"] is None for c in rev["corners"])
    for src in ("stored", "outline_curvature"):
        rev = derive_reverse(_with_corner_source(src), _reverse_ident())
        assert all("phases" not in c for c in rev["corners"]), src


def test_reverse_reverses_driven_line_cl_of_an_outline_boundary():
    # A capture parent: outline shape + its driven line `cl` (provenance outline+driven_line).
    # The backend measures sector positions along cl, so it must run in the reverse's travel order.
    fwd = _forward_record()
    fwd["boundary"] = {"source": "outline", "outline": [[0.0, 0.0], [100.0, 100.0]],
                       "cl": [[0.0, 0.0], [10.0, 0.0], [5.0, 8.0]]}
    rev = derive_reverse(fwd, _reverse_ident())
    validate_record(rev)
    # cl is a lap: the start/finish anchor (cl[0]) stays first, the rest runs the other way.
    assert rev["boundary"]["cl"] == [[0.0, 0.0], [5.0, 8.0], [10.0, 0.0]]
    assert rev["boundary"]["outline"] == [[0.0, 0.0], [100.0, 100.0]]  # undirected shape kept
    assert rev["boundary"]["source"] == "outline"
    assert fwd["boundary"]["cl"] == [[0.0, 0.0], [10.0, 0.0], [5.0, 8.0]]  # parent untouched


def test_reverse_keeps_closed_cl_start_finish_anchor():
    # A closed driven lap whose first and last samples are distinct (like grand-valley-south-2's,
    # 0.32 m apart): the backend measures sector positions from cl[0], the start/finish, so the
    # reverse must keep that point first and walk the rest of the lap the other way.
    fwd = _forward_record()
    square = [[0.0, 0.0], [10.0, 0.0], [10.0, 10.0], [0.0, 10.0]]
    fwd["boundary"] = {"source": "outline", "outline": [[0.0, 0.0], [100.0, 100.0]], "cl": square}
    rev = derive_reverse(fwd, _reverse_ident())
    assert rev["boundary"]["cl"] == [[0.0, 0.0], [0.0, 10.0], [10.0, 10.0], [10.0, 0.0]]


def test_reverse_keeps_explicitly_closed_cl_closed():
    # A cl that repeats its first point at the end stays explicitly closed on both ends.
    fwd = _forward_record()
    closed = [[0.0, 0.0], [10.0, 0.0], [10.0, 10.0], [0.0, 0.0]]
    fwd["boundary"] = {"source": "outline", "outline": [[0.0, 0.0], [100.0, 100.0]], "cl": closed}
    rev = derive_reverse(fwd, _reverse_ident())
    assert rev["boundary"]["cl"] == [[0.0, 0.0], [10.0, 10.0], [10.0, 0.0], [0.0, 0.0]]


def test_reverse_lap_short_cl_is_copied():
    # Fewer than 3 points can't describe a lap: copied unchanged (a 2-point "line" has no direction
    # worth anchoring), and never the same list object as the parent's.
    from rkb.reverse import _reverse_lap
    two = [[0.0, 0.0], [1.0, 0.0]]
    out = _reverse_lap(two)
    assert out == two and out is not two
