import json, glob, os, sqlite3, pytest

OUT = os.path.join(os.path.dirname(__file__), "..", "out")
pytestmark = pytest.mark.skipif(not os.path.isdir(os.path.join(OUT, "rkb")), reason="run `python -m rkb` first")


def test_121_json_records_and_db_consistent():
    files = glob.glob(os.path.join(OUT, "rkb", "*.rkb.json"))
    assert len(files) == 121
    from rkb.schema import validate_record
    for f in files:
        validate_record(json.load(open(f)))
    con = sqlite3.connect(os.path.join(OUT, "rkb.db"))
    assert con.execute("select count(*) from tracks").fetchone()[0] == 121


def test_hsr_reverse_reliable_and_six_corners():
    d = json.load(open(os.path.join(OUT, "rkb", "high-speed-ring-2.rkb.json")))
    assert len(d["corners"]) == 6
    assert d["confidence"]["coordinate_usability"] in ("generated-reliable", "generated-coarse")
