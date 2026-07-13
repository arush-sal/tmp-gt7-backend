from rkb.sectors import sectors_from_geometry
from rkb.affine import Affine

def test_parses_sector_turns_and_maps_labels():
    aff = Affine(scale=0.5, tx=0.0, tz=0.0)
    secs = sectors_from_geometry("S1=1-2; S2=2-5; S3=5-6",
                                 [{"sector":"S2","x":100,"y":50}], aff)
    by = {s["sector"]: s for s in secs}
    assert by["S1"]["turns"] == [1, 2]
    assert by["S2"]["turns"] == [2, 3, 4, 5]
    assert by["S2"]["label_world"] == [200.0, 100.0]
    assert by["S3"]["label_world"] is None    # no label provided
