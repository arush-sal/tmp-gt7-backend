def from_outline(outline_px, affine):
    return {"source": "outline", "outline": [list(affine.px_to_world(px, py)) for px, py in outline_px]}

def from_victorydash(cl, lb, rb):
    return {"source": "victorydash", "left": lb, "right": rb}
