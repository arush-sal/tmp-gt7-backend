"""Build the SQLite RKB DB — the backend's SOURCE OF TRUTH for track data.

The DB is authoritative and lossless: `tracks.record_json` holds the complete canonical
record (so nothing in the JSON is ever missing from the DB), and the flattened tables
(`tracks` scalar columns, `corners`, `sectors`, `boundary_points`, `width_profile`) are a
query-friendly view of that same record. Serving priority: DB > JSON > CSV. The per-track
JSON + index CSV are reference/generation artifacts only.
"""

import json
import os
import sqlite3

_SCHEMA = """
create table tracks(
  slug text primary key, name text, ddm_id int, hex_id text, base int,
  is_reverse int, is_oval int, length_m real, num_corners_expected int, width_m real, width_source text,
  affine_scale real, affine_rot_deg real, affine_tx real, affine_tz real, affine_source text,
  affine_rms_m real, affine_sf_anchor_err_m real, affine_scale_sanity_pct real,
  affine_world_bbox text, affine_svg_bbox text,
  boundary_source text,
  coordinate_usability text, physical_boundary_fidelity text, join_conf text,
  prov_affine text, prov_corners text, prov_boundary text, prov_width text,
  record_json text);
create table corners(
  slug text, turn int, x real, z real, radius_m real, direction text, type text, source text,
  apex_is_label int, heading_change_deg real, apex_speed_kmh real, lat_accel_g real,
  gear_at_apex int, banked int, phases_json text);
create table sectors(slug text, sector text, turns text, label_x real, label_z real);
create table boundary_points(slug text, side text, idx int, x real, z real);
create table width_profile(slug text, idx int, w_m real);
"""


def _b(v):
    """None-preserving bool -> int for sqlite."""
    return int(bool(v)) if v is not None else None


def _js(v):
    """Serialize a nested value to JSON text, or None."""
    return json.dumps(v) if v is not None else None


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
        "width_profile": 0,
    }
    for r in records:
        c = r["confidence"]
        a = r["affine"]
        w = r.get("width") or {}
        b = r.get("boundary") or {}
        prov = r.get("provenance") or {}
        con.execute(
            "insert into tracks("
            "slug,name,ddm_id,hex_id,base,is_reverse,is_oval,length_m,num_corners_expected,"
            "width_m,width_source,affine_scale,affine_rot_deg,affine_tx,affine_tz,affine_source,"
            "affine_rms_m,affine_sf_anchor_err_m,affine_scale_sanity_pct,affine_world_bbox,affine_svg_bbox,"
            "boundary_source,coordinate_usability,physical_boundary_fidelity,join_conf,"
            "prov_affine,prov_corners,prov_boundary,prov_width,record_json) "
            "values(" + ",".join(["?"] * 30) + ")",
            (
                r["slug"],
                r["name"],
                r.get("ddm_id"),
                r.get("hex_id"),
                r.get("base"),
                _b(r.get("is_reverse")),
                _b(r.get("is_oval")),
                r.get("length_m"),
                r.get("num_corners_expected"),
                w.get("width_m"),
                w.get("source"),
                a.get("scale"),
                a.get("rot_deg"),
                a.get("tx"),
                a.get("tz"),
                a.get("source"),
                a.get("rms_m"),
                a.get("sf_anchor_err_m"),
                a.get("scale_sanity_pct"),
                _js(a.get("world_bbox")),
                _js(a.get("svg_bbox")),
                b.get("source"),
                c["coordinate_usability"],
                c["physical_boundary_fidelity"],
                c["join"],
                prov.get("affine"),
                prov.get("corners"),
                prov.get("boundary"),
                prov.get("width"),
                json.dumps(r),  # full canonical record — DB is lossless
            ),
        )
        counts["tracks"] += 1
        for cor in r["corners"]:
            aw = cor.get("apex_world") or [None, None]
            con.execute(
                "insert into corners("
                "slug,turn,x,z,radius_m,direction,type,source,apex_is_label,"
                "heading_change_deg,apex_speed_kmh,lat_accel_g,gear_at_apex,banked,phases_json) "
                "values(" + ",".join(["?"] * 15) + ")",
                (
                    r["slug"],
                    cor.get("turn"),
                    aw[0],
                    aw[1],
                    cor.get("radius_m"),
                    cor.get("direction"),
                    cor.get("type"),
                    cor.get("source"),
                    _b(cor.get("apex_is_label")),
                    cor.get("heading_change_deg"),
                    cor.get("apex_speed_kmh"),
                    cor.get("lat_accel_g"),
                    cor.get("gear_at_apex"),
                    _b(cor.get("banked")),
                    _js(cor.get("phases")),
                ),
            )
            counts["corners"] += 1
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
        for side in ("left", "right", "outline"):
            for i, p in enumerate(b.get(side, []) or []):
                con.execute(
                    "insert into boundary_points values(?,?,?,?,?)",
                    (r["slug"], side, i, p[0], p[1]),
                )
                counts["boundary_points"] += 1
        for i, wv in enumerate(w.get("profile") or []):
            con.execute(
                "insert into width_profile values(?,?,?)", (r["slug"], i, wv)
            )
            counts["width_profile"] += 1
    con.commit()
    con.close()
    return counts
