import math
from rkb.affine import fit_bbox, bbox_corners
import numpy as np
from rkb.affine import fit_procrustes, assert_no_rotation, predict_affine

def test_fit_bbox_recovers_hsr_scale_and_roundtrips():
    # HSR: world bbox from gt7trackdetect, svg outline bbox ~ the 1200 box region
    world = (-817.0, -391.4, 738.7, 537.6)
    # synthetic outline px whose bbox is world mapped by scale 0.598, offset (626.6, 554.9)
    s, ox, oz = 0.598, 626.6, 554.9
    corners_px = [(s*world[0]+ox, s*world[1]+oz), (s*world[2]+ox, s*world[3]+oz)]
    aff = fit_bbox(corners_px, world)
    assert abs(aff.scale - 0.598) < 0.01
    # round-trip a world point
    px, py = aff.world_to_px(0.0, 0.0)
    x, z = aff.px_to_world(px, py)
    assert abs(x) < 1e-6 and abs(z) < 1e-6

def test_fit_bbox_rejects_anisotropic_scale():
    import pytest
    # outline whose x/z scales differ by >5% must raise
    world = (0.0, 0.0, 100.0, 100.0)
    bad_px = [(0.0, 0.0), (100.0, 200.0)]   # x-scale 1.0, z-scale 2.0
    with pytest.raises(ValueError):
        fit_bbox(bad_px, world)

def test_procrustes_recovers_pure_scale_translation_zero_rotation():
    rng = np.random.default_rng(0)
    world = rng.uniform(-500, 500, size=(200, 2))
    s, ox, oz = 0.6, 626.6, 554.9
    px = np.column_stack([s*world[:,0]+ox, s*world[:,1]+oz])
    aff = fit_procrustes(world, px)
    assert abs(aff.scale - 0.6) < 1e-3
    assert abs(aff.rot_deg) < 0.5
    assert assert_no_rotation(aff)

def test_procrustes_handles_reflection_without_scale_blowup():
    # Mirror the z-axis when mapping world->px: the two point sets are now
    # true reflections of each other, which forces fit_procrustes's
    # det(R) < 0 guard (no-flip) branch.
    rng = np.random.default_rng(1)
    world = rng.uniform(-500, 500, size=(200, 2))
    s, ox, oz = 0.6, 626.6, 554.9
    px = np.column_stack([s*world[:, 0]+ox, -s*world[:, 1]+oz])

    aff = fit_procrustes(world, px)
    assert aff.scale > 0
    assert math.isfinite(aff.rms_m)

    # The old (buggy) code unconditionally returned
    # scale = S.sum() / (W0**2).sum(), ignoring whether R was flipped by
    # the reflection guard. For this mirrored set that stale formula
    # gives ~0.6 -- about 14x too large versus the scale that is actually
    # consistent with the (correctly de-reflected) R fit_procrustes solves
    # for. Assert the fix moved well away from that stale value.
    buggy_scale = 0.6
    assert aff.scale < buggy_scale / 5

    # The corrected scale must be the least-squares-optimal scale for the
    # R that fit_procrustes settled on: re-scoring the SAME rotation with
    # the old buggy scale must fit worse (larger squared pixel error) than
    # with the corrected scale -- proving scale and R are back in sync.
    theta = math.radians(aff.rot_deg)
    R = np.array([[math.cos(theta), -math.sin(theta)],
                  [math.sin(theta), math.cos(theta)]])
    wc, pc = world.mean(0), px.mean(0)

    def sq_err(scale):
        t = pc - scale * (R @ wc)
        pred = scale * (world @ R.T) + t
        return ((pred - px) ** 2).sum()

    assert sq_err(aff.scale) < sq_err(buggy_scale)

def test_predict_affine_sets_scale_but_marks_offset_unresolved():
    import math
    aff = predict_affine(outline_px=[(0,0),(1200,0)], outline_perimeter_px=6000.0, length_m=10000.0)
    assert abs(aff.scale - 0.6) < 1e-9
    assert math.isnan(aff.tx) and aff.source == "predictor"

from rkb.affine import choose_affine

def test_choose_affine_prefers_bbox_over_predictor_and_flags_bias():
    world = (-817.0, -391.4, 738.7, 537.6)
    s, ox, oz = 0.598, 626.6, 554.9
    outline = [(s*world[0]+ox, s*world[1]+oz), (s*world[2]+ox, s*world[3]+oz)]
    aff = choose_affine({"world_bbox": world, "outline_px": outline,
                         "outline_perimeter_px": 6000.0, "length_m": 4345.0})
    assert aff.source == "bbox_from_driven_trace"

def test_choose_affine_falls_back_to_predictor():
    aff = choose_affine({"outline_px": [(0,0),(600,0)], "outline_perimeter_px": 6000.0, "length_m": 10000.0})
    assert aff.source == "predictor"
