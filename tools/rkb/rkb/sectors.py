import re

def _parse_turns(rng: str) -> list:
    """Parse a sector's turn-range spec into a flat, ordered list of turn numbers.

    Handles the formats seen across the KB's `sector_turns` strings:
      "1-2"        -> [1, 2]                  (inclusive range)
      "1"          -> [1]                     (single turn, no dash)
      "2, 4-5"     -> [2, 4, 5]               (comma-separated mix of singles/ranges,
                                                used e.g. where a sector wraps the S/F line)
      "Unknown"    -> []                       (KB extraction gap; no turns recorded)
    """
    turns = []
    for chunk in rng.split(","):
        chunk = chunk.strip()
        if not chunk or chunk.lower() == "unknown":
            continue
        if "-" in chunk:
            lo, hi = (int(x) for x in chunk.split("-"))
            turns.extend(range(lo, hi + 1))
        else:
            turns.append(int(chunk))
    return turns

def sectors_from_geometry(sector_turns, sector_labels, affine):
    labels = {l["sector"]: l for l in (sector_labels or [])}
    out = []
    for part in (sector_turns or "").split(";"):
        part = part.strip()
        if not part:
            continue
        name, rng = part.split("=")
        name = name.strip()
        turns = _parse_turns(rng.strip())
        lab = labels.get(name)
        lw = list(affine.px_to_world(float(lab["x"]), float(lab["y"]))) if lab else None
        out.append({"sector": name, "turns": turns, "label_world": lw})
    return out
