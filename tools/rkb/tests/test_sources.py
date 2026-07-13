from rkb.sources import parse_svg_outline, load_course, load_detect

def test_parse_svg_outline_extracts_points_and_stroke():
    svg = '<svg viewBox="0 0 1200 1200"><path id="track-outline" class="track-path" d="M 129 509 L 271 854 L 322 879 Z" stroke="#777" stroke-width="6"/></svg>'
    pts, stroke, vb = parse_svg_outline(svg)
    assert pts[0] == (129.0, 509.0) and pts[1] == (271.0, 854.0)
    assert stroke == 6.0
    assert vb == (0.0, 0.0, 1200.0, 1200.0)

def test_load_detect_keys_by_id_with_bbox_and_sf(tmp_path):
    p = tmp_path / "d.csv"
    p.write_text("TRACK,P1X,P1Y,P2X,P2Y,DIRECTION,MINX,MINY,MAXX,MAXY\n"
                 "11,75.7,-373.7,75.7,-391.4,\"NX\",-817.0,-391.4,738.7,537.6\n")
    d = load_detect(str(p))
    assert d[11]["bbox"] == (-817.0, -391.4, 738.7, 537.6)
    assert d[11]["sf"] == ((75.7, -373.7), (75.7, -391.4))
