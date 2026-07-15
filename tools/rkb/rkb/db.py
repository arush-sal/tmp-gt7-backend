"""Build the SQLite RKB DB — the backend's SOURCE OF TRUTH for track data.

The DB is normalized: `tracks`, `corners`, `corner_phases`, `sectors`, `boundary_points`,
and `reference_times` are scalar, self-documenting tables (no JSON blob column). The
Go `Store` reconstructs the canonical record from these tables and validates it
deep-equal against the per-track JSON. Serving priority: DB > JSON > CSV. The per-track
JSON + index CSV are reference/generation artifacts only.
"""

import os
import sqlite3

_SCHEMA = """
create table tracks(
  slug text primary key, name text, ddm_id int, hex_id text, base int,
  is_reverse int, is_oval int, length_m real, num_corners_expected int,
  width_m real, width_source text,
  affine_scale real, affine_rot_deg real, affine_tx real, affine_tz real, affine_source text,
  affine_rms_m real, affine_sf_anchor_err_m real, affine_scale_sanity_pct real,
  affine_world_bbox_min_x real, affine_world_bbox_min_z real,
  affine_world_bbox_max_x real, affine_world_bbox_max_z real,
  affine_svg_bbox_min_x real, affine_svg_bbox_min_y real,
  affine_svg_bbox_max_x real, affine_svg_bbox_max_y real,
  boundary_source text,
  coordinate_usability text, physical_boundary_fidelity text, join_confidence text,
  provenance_affine text, provenance_corners text, provenance_boundary text, provenance_width text);
create table corners(
  slug text, turn int, apex_x real, apex_z real, radius_m real, direction text, type text, source text,
  apex_is_label int, corner_angle_deg real, apex_speed_kmh real, lat_accel_g real,
  gear_at_apex int, banked int, banking_deg_at_apex real, length_m real);
create table corner_phases(
  slug text, turn int, phase text, world_x real, world_z real, phase_distance_from_apex real);
create table sectors(slug text, sector text, turns text, label_x real, label_z real);
create table boundary_points(slug text, side text, idx int, world_x real, world_z real);
create table reference_times(
  slug text, sector text, car_id int, car_category text, performance_point real,
  reference_time_ms int, unique(slug, sector, car_id, performance_point));
create index idx_corners_slug on corners(slug);
create index idx_corner_phases_slug on corner_phases(slug, turn);
create index idx_sectors_slug on sectors(slug);
create index idx_boundary_points_slug on boundary_points(slug);
create index idx_reference_times_slug on reference_times(slug);
"""


def _b(v):
    """None-preserving bool -> int for sqlite."""
    return int(bool(v)) if v is not None else None


def _bbox4(v):
    """Return a 4-tuple (a,b,c,d) from a [minx,miny,maxx,maxy]-style list, or (None,)*4."""
    if not v:
        return (None, None, None, None)
    return (v[0], v[1], v[2], v[3])


def build_db(records, path):
    if os.path.exists(path):
        os.remove(path)
    con = sqlite3.connect(path)
    con.executescript(_SCHEMA)
    counts = {
        "tracks": 0,
        "corners": 0,
        "sectors": 0,
        "boundary_points": 0,
    }
    for r in records:
        c = r["confidence"]
        a = r["affine"]
        w = r.get("width") or {}
        b = r.get("boundary") or {}
        prov = r.get("provenance") or {}
        wbx = _bbox4(a.get("world_bbox"))
        sbx = _bbox4(a.get("svg_bbox"))
        con.execute(
            "insert into tracks("
            "slug,name,ddm_id,hex_id,base,is_reverse,is_oval,length_m,num_corners_expected,"
            "width_m,width_source,"
            "affine_scale,affine_rot_deg,affine_tx,affine_tz,affine_source,"
            "affine_rms_m,affine_sf_anchor_err_m,affine_scale_sanity_pct,"
            "affine_world_bbox_min_x,affine_world_bbox_min_z,affine_world_bbox_max_x,affine_world_bbox_max_z,"
            "affine_svg_bbox_min_x,affine_svg_bbox_min_y,affine_svg_bbox_max_x,affine_svg_bbox_max_y,"
            "boundary_source,coordinate_usability,physical_boundary_fidelity,join_confidence,"
            "provenance_affine,provenance_corners,provenance_boundary,provenance_width) "
            "values(" + ",".join(["?"] * 35) + ")",
            (
                r["slug"], r["name"], r.get("ddm_id"), r.get("hex_id"), r.get("base"),
                _b(r.get("is_reverse")), _b(r.get("is_oval")), r.get("length_m"),
                r.get("num_corners_expected"),
                w.get("width_m"), w.get("source"),
                a.get("scale"), a.get("rot_deg"), a.get("tx"), a.get("tz"), a.get("source"),
                a.get("rms_m"), a.get("sf_anchor_err_m"), a.get("scale_sanity_pct"),
                wbx[0], wbx[1], wbx[2], wbx[3], sbx[0], sbx[1], sbx[2], sbx[3],
                b.get("source"), c["coordinate_usability"], c["physical_boundary_fidelity"], c["join"],
                prov.get("affine"), prov.get("corners"), prov.get("boundary"), prov.get("width"),
            ),
        )
        counts["tracks"] += 1
        for cor in r["corners"]:
            aw = cor.get("apex_world") or [None, None]
            con.execute(
                "insert into corners("
                "slug,turn,apex_x,apex_z,radius_m,direction,type,source,apex_is_label,"
                "corner_angle_deg,apex_speed_kmh,lat_accel_g,gear_at_apex,banked,"
                "banking_deg_at_apex,length_m) values(" + ",".join(["?"] * 16) + ")",
                (
                    r["slug"], cor.get("turn"), aw[0], aw[1], cor.get("radius_m"),
                    cor.get("direction"), cor.get("type"), cor.get("source"),
                    _b(cor.get("apex_is_label")), cor.get("heading_change_deg"),
                    cor.get("apex_speed_kmh"), cor.get("lat_accel_g"), cor.get("gear_at_apex"),
                    _b(cor.get("banked")), cor.get("roll_deg_at_apex"), cor.get("length_m"),
                ),
            )
            counts["corners"] += 1
            # phases may be: a dict (kinematic corners; braking_zone can be null), explicit None
            # (source='derived-reverse'), or absent (source in stored/outline_curvature). We store one
            # row per FULLY-NUMERIC phase point; the null-vs-absent phases state and any null
            # braking_zone are recoverable Go-side from corners.source + which rows exist.
            phases = cor.get("phases") or {}
            apex = phases.get("apex")
            apex_ok = apex is not None and apex[0] is not None and apex[1] is not None
            for phase_name in ("braking_zone", "turn_in", "apex", "exit"):
                pt = phases.get(phase_name)
                if pt is None or pt[0] is None or pt[1] is None:
                    continue  # skip missing/incomplete phase points (do not store, do not crash)
                dist = None  # NULL when apex is missing/null - never a misleading 0.0
                if apex_ok:
                    dist = ((pt[0] - apex[0]) ** 2 + (pt[1] - apex[1]) ** 2) ** 0.5
                con.execute(
                    "insert into corner_phases values(?,?,?,?,?,?)",
                    (r["slug"], cor.get("turn"), phase_name, pt[0], pt[1], dist),
                )
                counts.setdefault("corner_phases", 0)
                counts["corner_phases"] += 1
        for s in r["sectors"]:
            lw = s.get("label_world") or [None, None]
            con.execute(
                "insert into sectors values(?,?,?,?,?)",
                (
                    r["slug"],
                    s["sector"],
                    ",".join(map(str, s.get("turns", []))),
                    lw[0],
                    lw[1],
                ),
            )
            counts["sectors"] += 1
        for side in ("left", "right", "outline", "cl"):
            for i, p in enumerate(b.get(side, []) or []):
                con.execute(
                    "insert into boundary_points values(?,?,?,?,?)",
                    (r["slug"], side, i, p[0], p[1]),
                )
                counts["boundary_points"] += 1
    con.commit()
    con.close()
    return counts
