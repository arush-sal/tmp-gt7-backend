"""RKB record schema and validation."""

# NB: rkb.gate.coordinate_usability returns "generated-reliable"/"generated-coarse"/
# "needs-capture" (see rkb/gate.py); the bare "reliable"/"coarse" forms are kept for
# back-compat with existing callers/tests that pass them directly.
_USABILITY = {"reliable", "coarse", "needs-capture", "generated-reliable", "generated-coarse"}
_FIDELITY = {"measured", "derived", "none"}
_JOIN = {"exact", "fuzzy", "ambiguous"}
_REQUIRED = {
    "slug",
    "name",
    "affine",
    "corners",
    "sectors",
    "boundary",
    "width",
    "confidence",
    "provenance",
}


def validate_record(d: dict) -> None:
    """Validate RKB record dict.

    Raises ValueError if:
    - Required keys are missing
    - Confidence enum values are invalid
    """
    missing = _REQUIRED - d.keys()
    if missing:
        raise ValueError(f"record missing keys: {sorted(missing)}")

    c = d["confidence"]
    if c.get("coordinate_usability") not in _USABILITY:
        raise ValueError(f"bad coordinate_usability: {c.get('coordinate_usability')}")
    if c.get("physical_boundary_fidelity") not in _FIDELITY:
        raise ValueError(f"bad physical_boundary_fidelity: {c.get('physical_boundary_fidelity')}")
    if c.get("join") not in _JOIN:
        raise ValueError(f"bad join: {c.get('join')}")


def record_to_dict(ref, affine, corners, sectors, boundary, width, confidence, provenance) -> dict:
    """Assemble RKB record from ref object and components.

    Args:
        ref: Track reference object with slug, name, ddm_id, hex_id, base, is_reverse,
             is_oval, length_m, num_corners
        affine: Affine transform dict
        corners: List of corner dicts
        sectors: List of sector dicts
        boundary: Boundary dict
        width: Width dict (may carry internal measurement fields; only width_m/source
            are kept in the record -- see the trim below)
        confidence: Confidence dict
        provenance: Provenance dict

    Returns:
        Complete RKB record dict
    """
    return {
        "slug": ref.slug,
        "name": ref.name,
        "ddm_id": ref.ddm_id,
        "hex_id": ref.hex_id,
        "base": ref.base,
        "is_reverse": ref.is_reverse,
        "is_oval": getattr(ref, "is_oval", None),
        "length_m": ref.length_m,
        "num_corners_expected": ref.num_corners,
        "affine": affine,
        "corners": corners,
        "sectors": sectors,
        "boundary": boundary,
        # Width is a single scalar + its provenance -- callers (width.py, victorydash.py,
        # width_surface.py, width_calib.py) may compute internal fields (min_m/max_m/
        # profile/n_kept/n_total) for ranking/calibration, but only width_m and source
        # are part of the published record shape.
        "width": {"width_m": width.get("width_m"), "source": width.get("source")},
        "confidence": confidence,
        "provenance": provenance,
    }
