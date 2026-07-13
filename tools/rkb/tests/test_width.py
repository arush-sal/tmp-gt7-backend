from rkb.width import from_boundaries, from_svg_stroke, measure
from rkb.affine import Affine

def test_width_from_boundaries():
    lb = [[0.0, 0.0], [0.0, 10.0]]; rb = [[12.0, 0.0], [12.0, 10.0]]
    w = from_boundaries(lb, rb)
    assert w["width_m"] == 12.0 and w["source"] == "victorydash"

def test_width_from_svg_stroke():
    w = from_svg_stroke(6.0, Affine(scale=0.6, tx=0, tz=0))
    assert abs(w["width_m"] - 10.0) < 1e-9 and w["source"] == "svg_stroke"

def test_width_from_boundaries_discards_implausible_crossover_pairings():
    # Two good 12m cross-sections plus one crossover mis-pairing (0.2m -- the
    # nearest-neighbor aligner jumped to the OTHER side of a self-crossing
    # track) and one absurdly wide one (60m). Both implausible cross-sections
    # must be dropped from width_m/min_m/max_m/profile, not just clipped in.
    lb = [[0.0, 0.0], [0.0, 10.0], [0.0, 20.0], [0.0, 30.0]]
    rb = [[12.0, 0.0], [0.1, 10.0], [12.0, 20.0], [60.0, 30.0]]
    w = from_boundaries(lb, rb)
    assert w["source"] == "victorydash"
    assert w["min_m"] >= 3.0
    assert w["max_m"] <= 40.0
    assert w["width_m"] == 12.0
    assert w["profile"] == [12.0, 12.0]
    assert w["n_total"] == 4
    assert w["n_kept"] == 2

def test_measure_falls_back_to_svg_stroke_when_mostly_mispaired():
    # Only 1 of 6 cross-sections is plausible (~16.7%, below the 20% floor) --
    # a clear "mostly crossover mis-pairings" case that must not be trusted.
    lb = [[0.0, 0.0], [0.0, 10.0], [0.0, 20.0], [0.0, 30.0], [0.0, 40.0], [0.0, 50.0]]
    rb = [[12.0, 0.0], [0.1, 10.0], [0.1, 20.0], [0.1, 30.0], [0.1, 40.0], [0.1, 50.0]]
    aff = Affine(scale=0.6, tx=0, tz=0)
    w = measure(lb, rb, stroke_px=6.0, affine=aff)
    assert w["source"] == "svg_stroke"
    assert abs(w["width_m"] - 10.0) < 1e-9

def test_measure_uses_true_boundaries_when_enough_kept():
    lb = [[0.0, 0.0], [0.0, 10.0]]
    rb = [[12.0, 0.0], [12.0, 10.0]]
    w = measure(lb, rb, stroke_px=6.0, affine=Affine(scale=0.6, tx=0, tz=0))
    assert w["source"] == "victorydash"
    assert w["width_m"] == 12.0

def test_measure_without_fallback_inputs_returns_filtered_measurement():
    lb = [[0.0, 0.0], [0.0, 10.0], [0.0, 20.0], [0.0, 30.0], [0.0, 40.0], [0.0, 50.0]]
    rb = [[12.0, 0.0], [0.1, 10.0], [0.1, 20.0], [0.1, 30.0], [0.1, 40.0], [0.1, 50.0]]
    w = measure(lb, rb)
    assert w["source"] == "victorydash"
    assert w["n_kept"] == 1
