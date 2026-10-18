"""Derive a reverse-layout RKB record from its resolved forward twin.

A true forward/reverse pair is the same physical tarmac driven the opposite
way: the reverse shares the forward's affine, boundary shape, and width (identical
physical geometry; measured left/right edges swap sides), while corners reverse
order + flip direction, and sectors reverse order. This lets a reverse whose forward is resolved be derived with
no capture of its own.
"""

_FLIP = {"left": "right", "right": "left"}

# Corner sources whose turn order IS the direction of travel (a driven lap, or one
# already reversed from one). Must match rtps/rkb/detection.go trustedApexSources.
# Only these may yield "derived-reverse"; stored labels / outline curvature follow an
# SVG path's arbitrary order, so their reversal keeps the parent's (untrusted) source.
_TRUSTED_ORDER_SOURCES = ("kinematic", "derived-reverse")


def _reverse_source(src):
    return "derived-reverse" if src in _TRUSTED_ORDER_SOURCES else src

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
        # Kinematic corners emit heading_change_deg as an UNSIGNED magnitude
        # (direction is carried separately via `direction`, flipped above).
        # Negating it would produce a negative "magnitude"; keep it a magnitude.
        out["heading_change_deg"] = abs(out["heading_change_deg"])
    out["source"] = _reverse_source(corner.get("source"))
    if out["source"] == "derived-reverse":
        # phases contract (rkb.db / Go setPhases): derived-reverse -> explicit null;
        # stored / outline_curvature -> key absent (dropped above via _DROP_FIELDS).
        out["phases"] = None
    return out


def _derive_corners(forward_corners: list) -> list:
    reversed_corners = list(reversed(forward_corners))
    return [_flip_corner(c, i) for i, c in enumerate(reversed_corners, start=1)]


def _derive_sectors(forward_sectors: list, num_corners: int) -> list:
    reversed_sectors = list(reversed(forward_sectors))
    out = []
    for i, s in enumerate(reversed_sectors, start=1):
        s2 = dict(s)
        s2["sector"] = f"S{i}"
        turns = s.get("turns")
        if turns:
            # forward turn t -> reverse turn (N+1-t); re-sort ascending so the
            # list still reads in the (new, reverse) direction-of-travel order.
            s2["turns"] = sorted(num_corners + 1 - t for t in turns)
        out.append(s2)
    return out


def _reverse_lap(points: list) -> list:
    """Reverse a closed lap's travel direction while keeping its start/finish anchor.

    cl is a full lap starting at the start/finish line, and the backend measures sector positions
    from cl[0]. A plain list reversal would make the old LAST sample the new start (a lap's first
    and last samples are distinct points, e.g. 0.32 m apart on grand-valley-south-2), shifting every
    sector position. So cl[0] stays first and the rest of the lap is walked the other way; an
    explicitly closed lap (last point repeats the first) stays closed on both ends.
    """
    if len(points) < 3:
        return list(points)
    if points[0] == points[-1]:
        return [points[0]] + list(reversed(points[1:-1])) + [points[0]]
    return [points[0]] + list(reversed(points[1:]))


def _derive_boundary(boundary: dict) -> dict:
    """Directed boundary parts follow the reverse layout's own travel order.

    - `cl` (a measured centreline or a capture's driven line) is reversed, anchored at its start/finish (see _reverse_lap): the backend
      measures sector positions along cl from its first point, so it must run in this
      layout's direction of travel.
    - Measured left/right edges swap sides (driven the other way, the forward's right
      edge is the reverse's left) and are reversed into the reverse travel order.
    - An outline is an undirected SVG shape and is kept unchanged.
    """
    out = dict(boundary)
    if boundary.get("cl"):
        out["cl"] = _reverse_lap(boundary["cl"])
    if boundary.get("left") and boundary.get("right"):
        out["left"] = list(reversed(boundary["right"]))
        out["right"] = list(reversed(boundary["left"]))
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
    sectors = (
        own_sectors
        if own_sectors
        else _derive_sectors(forward_record["sectors"], len(forward_record["corners"]))
    )

    confidence = {
        "coordinate_usability": forward_record["confidence"]["coordinate_usability"],
        "physical_boundary_fidelity": forward_record["confidence"]["physical_boundary_fidelity"],
        "join": reverse_ident.get("join", "exact"),
    }
    provenance = {
        "affine": forward_record["provenance"]["affine"],
        "corners": _reverse_source(forward_record["provenance"]["corners"]),
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
        "boundary": _derive_boundary(forward_record["boundary"]),
        "width": forward_record["width"],
        "confidence": confidence,
        "provenance": provenance,
    }
    return record
