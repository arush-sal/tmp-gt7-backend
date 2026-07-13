"""Derive a reverse-layout RKB record from its resolved forward twin.

A true forward/reverse pair is the same physical tarmac driven the opposite
way: the reverse shares the forward's affine, boundary, and width (identical
physical geometry), while corners reverse order + flip direction, and sectors
reverse order. This lets a reverse whose forward is resolved be derived with
no capture of its own.
"""

_FLIP = {"left": "right", "right": "left"}

# Direction-specific corner fields: valid only for the direction actually
# driven, so they don't carry over to the reverse without a reverse-direction
# lap. Physical shape fields (apex_world, radius_m, type, turn) are kept.
_DROP_FIELDS = ("apex_speed_kmh", "lat_accel_g", "gear_at_apex", "phases")


def _flip_corner(corner: dict, turn: int) -> dict:
    out = {k: v for k, v in corner.items() if k not in _DROP_FIELDS}
    out["turn"] = turn
    if "direction" in out and out["direction"] in _FLIP:
        out["direction"] = _FLIP[out["direction"]]
    if out.get("heading_change_deg") is not None:
        out["heading_change_deg"] = -out["heading_change_deg"]
    out["phases"] = None
    out["source"] = "derived-reverse"
    return out


def _derive_corners(forward_corners: list) -> list:
    reversed_corners = list(reversed(forward_corners))
    return [_flip_corner(c, i) for i, c in enumerate(reversed_corners, start=1)]


def _derive_sectors(forward_sectors: list) -> list:
    reversed_sectors = list(reversed(forward_sectors))
    out = []
    for i, s in enumerate(reversed_sectors, start=1):
        s2 = dict(s)
        s2["sector"] = f"S{i}"
        out.append(s2)
    return out


def derive_reverse(forward_record: dict, reverse_ident: dict) -> dict:
    """Build the reverse layout's RKB record from its resolved forward twin.

    Args:
        forward_record: fully-resolved RKB record dict for the forward layout.
        reverse_ident: reverse track's own identity fields (slug, name, ddm_id,
            hex_id, base, length_m, num_corners_expected, is_reverse=True,
            join, and optionally its own `sectors` list). Any key present here
            wins for identity; `sectors` (if truthy) is used verbatim instead
            of deriving from the forward's.

    Returns:
        A dict that passes `rkb.schema.validate_record`.
    """
    corners = _derive_corners(forward_record["corners"])

    own_sectors = reverse_ident.get("sectors")
    sectors = own_sectors if own_sectors else _derive_sectors(forward_record["sectors"])

    confidence = {
        "coordinate_usability": forward_record["confidence"]["coordinate_usability"],
        "physical_boundary_fidelity": forward_record["confidence"]["physical_boundary_fidelity"],
        "join": reverse_ident.get("join", "exact"),
    }
    provenance = {
        "affine": forward_record["provenance"]["affine"],
        "corners": "derived-reverse",
        "boundary": forward_record["provenance"]["boundary"],
        "width": forward_record["provenance"]["width"],
    }

    record = {
        "slug": reverse_ident["slug"],
        "name": reverse_ident["name"],
        "ddm_id": reverse_ident.get("ddm_id"),
        "hex_id": reverse_ident.get("hex_id"),
        "base": reverse_ident.get("base"),
        "is_reverse": reverse_ident.get("is_reverse", True),
        "is_oval": reverse_ident.get("is_oval"),
        "length_m": reverse_ident.get("length_m"),
        "num_corners_expected": reverse_ident.get("num_corners_expected"),
        "affine": forward_record["affine"],
        "corners": corners,
        "sectors": sectors,
        "boundary": forward_record["boundary"],
        "width": forward_record["width"],
        "confidence": confidence,
        "provenance": provenance,
    }
    return record
