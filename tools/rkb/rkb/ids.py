from dataclasses import dataclass
from typing import Optional
from .names import normalize

@dataclass
class TrackRef:
    slug: str
    name: str
    ddm_id: Optional[int] = None
    hex_id: Optional[str] = None
    base: Optional[int] = None
    length_m: Optional[float] = None
    num_corners: Optional[int] = None
    is_reverse: Optional[bool] = None
    world_bbox: Optional[tuple] = None      # (minx, minz, maxx, maxz)
    sf_line: Optional[tuple] = None         # ((p1x,p1z),(p2x,p2z))
    join: str = "ambiguous"

def reconcile(kb_names: dict, detect_by_norm: dict, course_rows: list) -> dict:
    # index course rows by normalized name; a name colliding across >1 row (excluding reverse) -> ambiguous
    course_by_norm: dict[str, list] = {}
    for r in course_rows:
        course_by_norm.setdefault(normalize(r["Name"]), []).append(r)
    refs: dict[str, TrackRef] = {}
    for slug, name in kb_names.items():
        k = normalize(name)
        ref = TrackRef(slug=slug, name=name)
        rows = course_by_norm.get(k, [])
        if len(rows) == 1:
            r = rows[0]
            ref.ddm_id = int(r["ID"]); ref.base = int(r["Base"]); ref.length_m = float(r["Length"])
            ref.num_corners = int(r["NumCorners"]); ref.is_reverse = r["IsReverse"] == "1"
            ref.join = "exact"
        elif len(rows) > 1:
            ref.join = "ambiguous"
        else:
            ref.join = "fuzzy"   # kb-only, no course confirmation
        d = detect_by_norm.get(k)
        if d:
            ref.world_bbox = d["bbox"]; ref.sf_line = d["sf"]
        refs[slug] = ref
    return refs
