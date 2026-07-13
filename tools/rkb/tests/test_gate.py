from rkb.gate import coordinate_usability, physical_boundary_fidelity, corner_count_ok
from rkb.affine import Affine

def test_needs_capture_when_offset_unresolved():
    import math
    aff = Affine(scale=0.6, tx=math.nan, tz=math.nan, source="predictor")
    assert coordinate_usability(aff, join="exact", corner_match=False) == "needs-capture"

def test_reliable_on_good_bbox_with_sf_and_corner_match():
    aff = Affine(scale=0.6, tx=100, tz=100, source="bbox_from_driven_trace",
                 sf_anchor_err_m=6.0, scale_sanity_pct=1.0)
    assert coordinate_usability(aff, join="exact", corner_match=True) == "generated-reliable"

def test_victorydash_reliable_regardless_of_high_rms():
    # VictoryDash supplies authoritative world coords; the SVG affine's own
    # inaccuracy (high rms) must NOT cap the tier when join is exact + corners match.
    aff = Affine(scale=0.6, tx=1, tz=2, source="victorydash", rms_m=40.0)
    assert coordinate_usability(aff, join="exact", corner_match=True) == "generated-reliable"

def test_victorydash_reliable_even_without_corner_match():
    # coordinate_usability is a COORDINATE-placement measure; for victorydash it must not
    # hinge on corner_match (a detector/track-shape concern) — e.g. Interlagos: 9 detected
    # vs 15 official is fine, the coordinates are still authoritative.
    aff = Affine(scale=0.6, tx=1, tz=2, source="victorydash", rms_m=40.0)
    assert coordinate_usability(aff, join="exact", corner_match=False) == "generated-reliable"

def test_victorydash_not_reliable_without_exact_join():
    aff = Affine(scale=0.6, tx=1, tz=2, source="victorydash", rms_m=40.0)
    assert coordinate_usability(aff, join="fuzzy", corner_match=True) == "generated-coarse"

def test_corner_count_tolerances():
    assert corner_count_ok(6, 6, "kinematic") is True
    assert corner_count_ok(72, 73, "kinematic") is True     # +-1
    assert corner_count_ok(70, 73, "stored") is False        # stored must be exact
    assert corner_count_ok(70, 73, "outline_curvature") is True  # <=10%

def test_fidelity_measured_only_for_victorydash():
    assert physical_boundary_fidelity("victorydash", "victorydash") == "measured"
    assert physical_boundary_fidelity("outline", "svg_stroke") == "derived"
