import csv

_COLS = [
    "slug",
    "ddm_id",
    "hex_id",
    "name",
    "base",
    "is_reverse",
    "length_m",
    "num_corners",
    "width_m",
    "affine_scale",
    "coordinate_usability",
    "physical_boundary_fidelity",
    "join",
    "needs_capture",
]


def write_index(records, path):
    with open(path, "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(_COLS)
        for r in records:
            c = r["confidence"]
            w.writerow(
                [
                    r["slug"],
                    r.get("ddm_id"),
                    r.get("hex_id"),
                    r["name"],
                    r.get("base"),
                    r.get("is_reverse"),
                    r.get("length_m"),
                    r.get("num_corners_expected"),
                    (r.get("width") or {}).get("width_m")
                    if isinstance(r.get("width"), dict)
                    else r.get("width_m"),
                    r["affine"].get("scale"),
                    c["coordinate_usability"],
                    c["physical_boundary_fidelity"],
                    c["join"],
                    c["coordinate_usability"] == "needs-capture",
                ]
            )
