"""Track RKB record orchestrator: wires affine, corners, sectors, boundary,
width, and the confidence gate into one record per track, plus the batch
driver (`build_all`) that reconciles the knowledge base against course/detect
externals and writes the on-disk RKB artifacts."""
import json
import math
import os
import re

from . import affine as affine_mod
from . import boundary as boundary_mod
from . import corners as corners_mod
from . import db as db_mod
from . import gate as gate_mod
from . import index as index_mod
from . import reverse as reverse_mod
from . import schema as schema_mod
from . import sectors as sectors_mod
from . import sources as sources_mod
from . import victorydash as victorydash_mod
from . import width as width_mod
from .ids import reconcile
from .names import normalize


def _outline_perimeter_px(outline_px):
    if not outline_px or len(outline_px) < 2:
        return 0.0
    total = 0.0
    for (x0, y0), (x1, y1) in zip(outline_px, outline_px[1:]):
        total += math.hypot(x1 - x0, y1 - y0)
    return total


def _json_safe(obj):
    """Recursively map every non-finite float (NaN, +Inf, -Inf) to None so the
    record serializes under strict JSON. Go's encoding/json rejects bare NaN/Inf
    tokens, so these must never reach json.dump. Everything else is left intact."""
    if isinstance(obj, float):
        return obj if math.isfinite(obj) else None
    if isinstance(obj, dict):
        return {k: _json_safe(v) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [_json_safe(v) for v in obj]
    return obj


def build_track(ref, kb_entry, telemetry_rows=None, victorydash=None, vd_dir=None) -> dict:
    outline_px = kb_entry.get("outline_px") or []
    outline_perimeter_px = _outline_perimeter_px(outline_px)

    driven_world_pts = None
    if telemetry_rows:
        driven_world_pts = [(float(r["pos_x"]), float(r["pos_z"])) for r in telemetry_rows]

    victorydash_pts = None
    if victorydash is not None:
        victorydash_pts = list(victorydash.get("left") or []) + list(victorydash.get("right") or [])

    if vd_dir is not None:
        # VictoryDash replay override: true measured boundary/width + a bbox-fit affine
        # (uniform scale+translation, no rotation — see rkb.victorydash.affine_from_vd)
        # + corners kinematically detected off the replay's own driven lap.
        boundary = victorydash_mod.boundary_from_vd(vd_dir)
        width = victorydash_mod.width_from_vd(vd_dir)
        aff = victorydash_mod.affine_from_vd(vd_dir, outline_px)
        corners = victorydash_mod.corners_from_vd(vd_dir)
    else:
        affine_inp = {
            "victorydash_pts": victorydash_pts,
            "world_bbox": ref.world_bbox,
            "sf_line": ref.sf_line,
            "svg_sf_px": None,  # geometry.json has no S/F px marker
            "outline_px": outline_px,
            "outline_perimeter_px": outline_perimeter_px,
            "driven_world_pts": driven_world_pts,
            "length_m": ref.length_m,
        }
        aff = affine_mod.choose_affine(affine_inp)

        if telemetry_rows:
            corners = corners_mod.detect_corners(telemetry_rows)
        elif kb_entry.get("turns"):
            corners = corners_mod.corners_from_stored(kb_entry["turns"], aff)
        else:
            corners = corners_mod.corners_from_outline(outline_px, aff)

        if victorydash is not None:
            boundary = boundary_mod.from_victorydash(
                victorydash.get("centerline"), victorydash.get("left"), victorydash.get("right")
            )
            width = width_mod.from_boundaries(victorydash.get("left"), victorydash.get("right"))
        else:
            boundary = boundary_mod.from_outline(outline_px, aff)
            width = width_mod.from_svg_stroke(kb_entry.get("outline_stroke_px", 0.0), aff)

    sectors = sectors_mod.sectors_from_geometry(
        kb_entry.get("sector_turns"), kb_entry.get("sector_labels"), aff
    )

    corner_src = corners[0]["source"] if corners else "outline_curvature"
    corner_match = gate_mod.corner_count_ok(len(corners), ref.num_corners, corner_src)
    confidence = {
        "coordinate_usability": gate_mod.coordinate_usability(aff, ref.join, corner_match),
        "physical_boundary_fidelity": gate_mod.physical_boundary_fidelity(
            boundary["source"], width["source"]
        ),
        "join": ref.join,
    }
    provenance = {
        "affine": aff.source,
        "corners": corner_src,
        "boundary": boundary["source"],
        "width": width["source"],
    }
    affine_dict = {
        "scale": aff.scale,
        "rot_deg": aff.rot_deg,
        "tx": aff.tx,
        "tz": aff.tz,
        "source": aff.source,
        "rms_m": getattr(aff, "rms_m", None),
        "sf_anchor_err_m": getattr(aff, "sf_anchor_err_m", None),
        "scale_sanity_pct": getattr(aff, "scale_sanity_pct", None),
        "world_bbox": getattr(aff, "world_bbox", None),
        "svg_bbox": getattr(aff, "svg_bbox", None),
    }

    record = schema_mod.record_to_dict(
        ref, affine_dict, corners, sectors, boundary, width, confidence, provenance
    )
    schema_mod.validate_record(record)
    return record


def _detect_by_norm(course_rows, detect_by_id):
    """Bridge gt7trackdetect (keyed by numeric ddm TRACK id) to reconcile's expected
    keying (normalized course Name) via the course.csv ID<->Name join."""
    out = {}
    for row in course_rows:
        try:
            tid = int(row["ID"])
        except (KeyError, ValueError, TypeError):
            continue
        if tid in detect_by_id:
            out[normalize(row["Name"])] = detect_by_id[tid]
    return out


_VARIANT_SUFFIX = re.compile(r"\s+(reverse|clockwise|counterclockwise)\s*$", re.I)

_RESOLVED_AFFINE_SOURCES = {"victorydash", "bbox_from_driven_trace"}


def _pair_core_name(name: str) -> str:
    """Strip the direction-variant suffix (" Reverse" / " Clockwise" /
    " Counterclockwise") from a track name and normalize what remains, so a
    forward/reverse pair (or a cw/ccw pair) collapses to the same key."""
    return normalize(_VARIANT_SUFFIX.sub("", name))


def _apply_reverse_pass(records: dict) -> None:
    """Post-pass: for each true forward/reverse pair where exactly one member is
    resolved (affine source victorydash/bbox_from_driven_trace) and the other is
    not (needs-capture/predictor), replace the weaker member's record in-place
    with one derived from the stronger member via `reverse.derive_reverse`."""
    groups: dict = {}
    for slug, rec in records.items():
        if rec.get("base") is None:
            continue
        key = (rec["base"], _pair_core_name(rec["name"]))
        groups.setdefault(key, []).append(slug)

    for slugs in groups.values():
        if len(slugs) != 2:
            continue
        a, b = slugs
        rec_a, rec_b = records[a], records[b]
        a_resolved = rec_a["provenance"]["affine"] in _RESOLVED_AFFINE_SOURCES
        b_resolved = rec_b["provenance"]["affine"] in _RESOLVED_AFFINE_SOURCES
        if a_resolved == b_resolved:
            continue  # both resolved, both weak, or ambiguous - nothing to derive
        strong_slug, weak_slug = (a, b) if a_resolved else (b, a)
        strong_rec, weak_rec = records[strong_slug], records[weak_slug]

        reverse_ident = {
            "slug": weak_rec["slug"],
            "name": weak_rec["name"],
            "ddm_id": weak_rec["ddm_id"],
            "hex_id": weak_rec["hex_id"],
            "base": weak_rec["base"],
            "length_m": weak_rec["length_m"],
            "num_corners_expected": weak_rec["num_corners_expected"],
            "is_reverse": weak_rec["is_reverse"],
            "is_oval": weak_rec.get("is_oval"),
            "join": weak_rec["confidence"]["join"],
            "sectors": weak_rec.get("sectors"),
        }
        derived = reverse_mod.derive_reverse(strong_rec, reverse_ident)
        schema_mod.validate_record(derived)
        records[weak_slug] = derived


def build_all(kb_dir, course_csv, detect_csv, telemetry_map, vd_map, out_dir) -> None:
    kb = sources_mod.load_kb(kb_dir)
    kb_names = {slug: entry["name"] for slug, entry in kb.items()}

    course_rows = sources_mod.load_course(course_csv) if os.path.exists(course_csv) else []
    detect_by_id = sources_mod.load_detect(detect_csv) if os.path.exists(detect_csv) else {}
    detect_by_norm = _detect_by_norm(course_rows, detect_by_id)

    refs = reconcile(kb_names, detect_by_norm, course_rows)

    rkb_dir = os.path.join(out_dir, "rkb")
    os.makedirs(rkb_dir, exist_ok=True)

    records = {}
    vd_map = vd_map or {}
    for slug, ref in refs.items():
        try:
            record = build_track(
                ref,
                kb[slug],
                telemetry_rows=telemetry_map.get(slug),
                vd_dir=vd_map.get(slug),
            )
        except Exception as exc:  # noqa: BLE001 - degrade gracefully when externals are missing
            print(f"skipping {slug}: {exc}")
            continue
        records[slug] = record

    _apply_reverse_pass(records)

    final_records = []
    for slug, record in records.items():
        # Sanitize the ENTIRE record: NaN/Inf flows from a needs-capture affine
        # (tx=tz=NaN) through px_to_world into apex_world, boundary.outline,
        # width_m, sectors[].label_world. Map every non-finite float to null so
        # the canonical JSON stays strict-parseable (Go's encoding/json rejects
        # bare NaN) and the derived index/db agree with it. allow_nan=False is a
        # belt-and-suspenders guard: with the sanitize in place it can never
        # fire, but a future regression fails loud instead of emitting NaN.
        record = _json_safe(record)
        final_records.append(record)
        with open(os.path.join(rkb_dir, f"{slug}.rkb.json"), "w") as f:
            json.dump(record, f, indent=2, allow_nan=False)

    index_mod.write_index(final_records, os.path.join(out_dir, "rkb-index.csv"))
    db_mod.build_db(final_records, os.path.join(out_dir, "rkb.db"))
