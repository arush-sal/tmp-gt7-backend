from rkb.width import from_boundaries, from_svg_stroke
from rkb.affine import Affine

def test_width_from_boundaries():
    lb = [[0.0, 0.0], [0.0, 10.0]]; rb = [[12.0, 0.0], [12.0, 10.0]]
    w = from_boundaries(lb, rb)
    assert w["width_m"] == 12.0 and w["source"] == "victorydash"

def test_width_from_svg_stroke():
    w = from_svg_stroke(6.0, Affine(scale=0.6, tx=0, tz=0))
    assert abs(w["width_m"] - 10.0) < 1e-9 and w["source"] == "svg_stroke"
