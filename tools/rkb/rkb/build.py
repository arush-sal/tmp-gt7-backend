"""Track RKB record orchestrator: wires affine, corners, sectors, boundary,
width, and the confidence gate into one record per track, plus the batch
driver (`build_all`) that reconciles the knowledge base against course/detect
externals and writes the on-disk RKB artifacts."""
import copy
import csv
import json
import math
import os
import re

from . import affine as affine_mod
from . import boundary as boundary_mod
from . import capture as capture_mod
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
from . import width_calib as width_calib_mod
from . import width_surface as width_surface_mod
from .ids import reconcile
from .names import normalize

# Sardegna-road-a is the ONLY track with both a VictoryDash true width AND a width-lap
# surface_type capture for the same car -- the calibration anchor (see rkb.width_calib).
_WIDTH_CALIBRATION_ANCHOR_SLUG = "sardegna-road-a"


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


def _resolve_capture_width(default_width, width_lap_rows, calibration_table):
    """Prefer a calibrated `telemetry_width_lap` measurement over `default_width`
    (the SVG-stroke fallback) when the width-lap capture yields a usable measurement;
    otherwise return `default_width` unchanged (graceful degradation -- see
    rkb.width_calib.calibrated_width for the calibration/publishability rule)."""
    if not width_lap_rows:
        return default_width
    raw = width_surface_mod.width_from_surface(width_lap_rows)
    if raw["width_m"] is None:
        return default_width
    car_category = width_lap_rows[0].get("car_category")
    calibrated, publishable = width_calib_mod.calibrated_width(
        raw, car_category, calibration_table or {}
    )
    print(
        f"width (telemetry_width_lap): {calibrated['width_m']}m car={car_category} "
        f"publishable_measured={publishable} n_kept={calibrated['n_kept']}/{calibrated['n_total']}"
    )
    return calibrated


def _log_vd_surface_crosscheck(slug, width_lap_rows, vd_width_dict):
    """Sardegna-road-a keeps its VictoryDash-measured width (true, preferred) -- this
    only cross-checks the surface_type raw measurement against it and logs the delta,
    per spec ("use surface_type only to cross-check")."""
    if not width_lap_rows:
        return
    raw = width_surface_mod.width_from_surface(width_lap_rows)
    if raw["width_m"] is None or vd_width_dict.get("width_m") is None:
        return
    delta = round(raw["width_m"] - vd_width_dict["width_m"], 3)
    print(
        f"{slug}: surface_type-vs-victorydash width delta={delta}m "
        f"(surface_type={raw['width_m']}m victorydash={vd_width_dict['width_m']}m)"
    )


def build_track(
    ref, kb_entry, telemetry_rows=None, victorydash=None, vd_dir=None, capture_csv=None,
    width_lap_rows=None, calibration_table=None,
) -> dict:
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
        aff = victorydash_mod.affine_from_vd(vd_dir, outline_px)
        width = victorydash_mod.width_from_vd(
            vd_dir, outline_stroke_px=kb_entry.get("outline_stroke_px", 0.0), affine=aff
        )
        corners = victorydash_mod.corners_from_vd(vd_dir)
        # This slug (sardegna-road-a) also has its own width-lap capture -- VictoryDash
        # width is true/measured and stays authoritative; surface_type is only logged as
        # a cross-check, never used to override it (spec: "PREFER VictoryDash").
        _log_vd_surface_crosscheck(ref.slug, width_lap_rows, width)
    elif capture_csv is not None:
        # Our own pcap capture: authoritative driven world coordinates resolve the
        # affine (bbox fit against the KB SVG outline, like the bbox_from_driven_trace
        # tier) and feed the kinematic corner detector -- but there is no separate
        # measured boundary ground truth for our captures, so boundary still comes from
        # the SVG outline (derived), same as the generic KB-only path. Width defaults to
        # the SVG-stroke fallback too, UNLESS a width-lap capture is available and
        # calibratable (_resolve_capture_width) -- a measured scalar width with no
        # independent centreline still can't fabricate a boundary (spec: "don't
        # fabricate boundaries"), so boundary stays SVG-derived regardless.
        aff = capture_mod.affine_from_capture(capture_csv, outline_px)
        corners = capture_mod.corners_from_capture(capture_csv)
        boundary = boundary_mod.from_outline(outline_px, aff)
        width = width_mod.from_svg_stroke(kb_entry.get("outline_stroke_px", 0.0), aff)
        width = _resolve_capture_width(width, width_lap_rows, calibration_table)
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

_RESOLVED_AFFINE_SOURCES = {"victorydash", "bbox_from_driven_trace", "capture"}


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


# A track's width is a property of the BASE — constant along its length and shared by every
# layout (forward + reverse). Rank the width evidence so a base adopts its single best
# measurement: VictoryDash true edge-to-edge > telemetry width-lap > SVG-stroke fallback.
_WIDTH_SOURCE_PRIORITY = {"victorydash": 3, "telemetry_width_lap": 2, "svg_stroke": 1}

# Some GT7 VENUES span more than one `base` group but are the same physical tarmac, so one
# width covers the whole venue (user rule: Sardegna Windmills shares the Sardegna Road width,
# even though windmills is a distinct base). A slug matching one of these prefixes joins a
# single venue-wide width group; every other slug's width group is just its own `base`.
_WIDTH_VENUE_PREFIXES = ("sardegna-",)


def _width_group_key(rec: dict):
    """The key identifying which layouts share ONE width. A venue prefix (see
    _WIDTH_VENUE_PREFIXES) unions all its layouts across bases; otherwise the group is the
    layout's `base`. Returns None for a baseless, non-venue track (no siblings to share
    with -- left untouched by the width pass)."""
    slug = rec.get("slug") or ""
    for pref in _WIDTH_VENUE_PREFIXES:
        if slug.startswith(pref):
            return ("venue", pref)
    base = rec.get("base")
    return ("base", base) if base is not None else None


def _apply_base_width_pass(records: dict) -> None:
    """Post-pass: a track's width is a property of the base (and, for a few venues, of the
    whole venue) -- one value shared by every layout, forward and reverse (a group can never
    carry divergent per-layout widths). Per width-group (_width_group_key), pick the single
    best-evidence width (victorydash > telemetry_width_lap > svg_stroke) among the layouts
    that have a real measurement and stamp it on every layout of that group. Recomputes
    physical_boundary_fidelity per layout (it depends on the width source, which may change
    here) via the same gate used at build time."""
    by_group: dict = {}
    for slug, rec in records.items():
        key = _width_group_key(rec)
        if key is None:
            continue  # baseless, non-venue track has no sibling layouts to share a width with
        by_group.setdefault(key, []).append(slug)

    for slugs in by_group.values():
        best_width, best_rank = None, -1
        for slug in slugs:
            w = records[slug].get("width") or {}
            if w.get("width_m") is None:
                continue
            rank = _WIDTH_SOURCE_PRIORITY.get(w.get("source"), 0)
            if rank > best_rank:
                best_rank, best_width = rank, w
        if best_width is None:
            continue  # no layout of this base has a usable width — leave all as-is
        for slug in slugs:
            rec = records[slug]
            # deepcopy: layouts must not alias one width dict OR its nested `profile`
            # (a list for svg_stroke, a dict for telemetry_width_lap).
            rec["width"] = copy.deepcopy(best_width)
            rec["provenance"]["width"] = best_width["source"]
            rec["confidence"]["physical_boundary_fidelity"] = gate_mod.physical_boundary_fidelity(
                rec["boundary"]["source"], best_width["source"]
            )
            schema_mod.validate_record(rec)


def _load_width_lap_map(width_lap_slugs, capture_csv_map):
    """For each slug in `width_lap_slugs` that has a resolved capture CSV, load it and
    pick out its deliberate width-hugging lap (rkb.width_calib.select_width_lap).
    Degrades gracefully (skips the slug) when the CSV is missing/unreadable/has no
    lap meeting the minimum-row floor."""
    width_lap_map = {}
    for slug in width_lap_slugs or ():
        csv_path = capture_csv_map.get(slug)
        if not csv_path or not os.path.isfile(csv_path):
            continue
        try:
            with open(csv_path) as f:
                rows = list(csv.DictReader(f))
            lap_rows = width_calib_mod.select_width_lap(rows)
        except Exception as exc:  # noqa: BLE001 - degrade gracefully when a capture is bad
            print(f"skipping width-lap load for {slug}: {exc}")
            continue
        if lap_rows:
            width_lap_map[slug] = lap_rows
    return width_lap_map


def _build_width_calibration_table(width_lap_map, vd_map):
    """Derive the (single-anchor, per-car) width calibration table from
    `_WIDTH_CALIBRATION_ANCHOR_SLUG` (sardegna-road-a): the one track with both a
    width-lap capture and an independent VictoryDash true width. Returns
    {car_category: calibration_entry}; {} if the anchor's data isn't available."""
    anchor_rows = width_lap_map.get(_WIDTH_CALIBRATION_ANCHOR_SLUG)
    anchor_vd_dir = (vd_map or {}).get(_WIDTH_CALIBRATION_ANCHOR_SLUG)
    if not anchor_rows or not anchor_vd_dir:
        return {}
    try:
        anchor_raw = width_surface_mod.width_from_surface(anchor_rows)
        if anchor_raw["width_m"] is None:
            return {}
        car_category = anchor_rows[0].get("car_category")
        entry = width_calib_mod.build_calibration(
            anchor_vd_dir, anchor_raw["width_m"], car_category, _WIDTH_CALIBRATION_ANCHOR_SLUG
        )
        print(
            f"width calibration ({car_category}, anchor={_WIDTH_CALIBRATION_ANCHOR_SLUG}): "
            f"W_true={entry['w_true_m']}m W_raw={entry['w_raw_m']}m "
            f"correction={entry['correction_m']}m confidence={entry['confidence']}"
        )
        return {car_category: entry}
    except Exception as exc:  # noqa: BLE001 - degrade gracefully when the anchor is bad
        print(f"skipping width calibration: {exc}")
        return {}


def build_all(
    kb_dir, course_csv, detect_csv, telemetry_map, vd_map, out_dir, capture_map=None,
    capture_csv_overrides=None, width_lap_slugs=None,
) -> None:
    kb = sources_mod.load_kb(kb_dir)
    kb_names = {slug: entry["name"] for slug, entry in kb.items()}

    course_rows = sources_mod.load_course(course_csv) if os.path.exists(course_csv) else []
    detect_by_id = sources_mod.load_detect(detect_csv) if os.path.exists(detect_csv) else {}
    detect_by_norm = _detect_by_norm(course_rows, detect_by_id)

    refs = reconcile(kb_names, detect_by_norm, course_rows)

    rkb_dir = os.path.join(out_dir, "rkb")
    os.makedirs(rkb_dir, exist_ok=True)

    # Decode each of our own pcap captures to CSV once up front, so build_track
    # (called per-slug below) never re-invokes the Go decoder. `capture_csv_overrides`
    # seeds already-resolved slug->csv paths (e.g. a multi-layout capture split by
    # rkb.capture.capture_csvs_for_multi_layout, decoded+split once by the caller)
    # before the single-layout capture_map entries are decoded below.
    capture_map = capture_map or {}
    capture_csv_map = dict(capture_csv_overrides or {})
    for slug, pcap_path in capture_map.items():
        try:
            capture_csv_map[slug] = capture_mod.decode_capture(pcap_path)
        except Exception as exc:  # noqa: BLE001 - degrade gracefully when a capture is missing/bad
            print(f"skipping capture decode for {slug}: {exc}")

    vd_map = vd_map or {}
    width_lap_map = _load_width_lap_map(width_lap_slugs, capture_csv_map)
    calibration_table = _build_width_calibration_table(width_lap_map, vd_map)

    records = {}
    for slug, ref in refs.items():
        try:
            record = build_track(
                ref,
                kb[slug],
                telemetry_rows=telemetry_map.get(slug),
                vd_dir=vd_map.get(slug),
                capture_csv=capture_csv_map.get(slug),
                width_lap_rows=width_lap_map.get(slug),
                calibration_table=calibration_table,
            )
        except Exception as exc:  # noqa: BLE001 - degrade gracefully when externals are missing
            print(f"skipping {slug}: {exc}")
            continue
        records[slug] = record

    _apply_reverse_pass(records)
    # Width is a base property: after reverse-derivation (which copies the forward's width),
    # unify every base's layouts onto its single best-evidence width. Runs last so derived
    # reverse records are stamped too.
    _apply_base_width_pass(records)

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
