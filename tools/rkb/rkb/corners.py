"""
Kinematic corner derivation from FULL-FIELD GT7 telemetry (the real capture, not the
stripped golden). Corner radius/apex/direction come from the velocity-vector heading
rate (NOT differentiated position, NOT body-frame yaw); the 4-phase model comes from
brake/throttle.

This is a faithful port of the proven prototype (`rkb/derive_kinematic.py`) into
importable functions. The algorithm, thresholds, and guards are unchanged.

  radius R      = speed_ms / |heading_rate|
  lateral accel = speed_ms * |heading_rate|      (== v^2 / R)
  apex          = cumulative-heading MIDPOINT of the corner run (not max curvature)
  braking zone  = brake onset before apex
  turn-in       = start of the sign-aware corner run
  exit          = throttle reapplied / where the run ends
"""
import math
import numpy as np


def _col(rows, name, cast=float):
    return np.array([cast(r[name]) for r in rows])


def _bbox(px, pz, idx):
    return (px[idx].min(), px[idx].max(), pz[idx].min(), pz[idx].max())


def on_track(row) -> bool:
    """True when a telemetry row is the car actually driving on track.

    Mirrors the backend's gate (on_track = in_race OR current_lap >= 0, plus a
    must-move rule): current GT7 versions never set in_race (flag 0x8E bit0), so
    in_race=false rows still count when a lap is running (current_lap >= 0; menus
    report -1) AND the car is moving (> 5 km/h; grid/pause frames are excluded).
    Older captures that DO set in_race=true behave exactly as before.
    """
    if row.get("in_race") == "true":
        return True
    try:
        return int(row["current_lap"]) >= 0 and float(row["car_speed"]) > 5.0
    except (KeyError, TypeError, ValueError):
        return False


def select_lap(rows):
    """Pick the clean flying lap: closed loop, big x/z-span, plausible frame count.

    Returns a numpy array of row indices (into `rows`) for the selected lap.
    """
    pid = _col(rows, "package_id", int)
    lap = _col(rows, "current_lap", int)
    inr = np.array([on_track(r) for r in rows])
    px = _col(rows, "pos_x")
    pz = _col(rows, "pos_z")

    # --- segment into contiguous lap runs: break on lap change, pid gap, or not on track ---
    runs = []
    cur = []
    for i in range(len(rows)):
        brk_seg = (not inr[i]) or (i > 0 and (lap[i] != lap[i - 1] or pid[i] - pid[i - 1] > 10 or pid[i] < pid[i - 1]))
        if brk_seg and cur:
            runs.append(cur)
            cur = []
        if inr[i]:
            cur.append(i)
    if cur:
        runs.append(cur)

    # choose the clean flying lap: closed loop, big x-span, plausible frame count
    cands = []
    for r in runs:
        idx = np.array(r)
        if len(idx) < 2500 or len(idx) > 45000:
            continue   # cap raised for long tracks (Nordschleife ~30k)
        x0, x1, z0, z1 = _bbox(px, pz, idx)
        closed = math.hypot(px[idx[0]] - px[idx[-1]], pz[idx[0]] - pz[idx[-1]])
        span = (x1 - x0) + (z1 - z0)
        if span < 400:
            continue   # reject stationary/pit clusters (spawn idle etc.) — not a driven lap
        cands.append((idx, len(idx), closed, span, (x0, x1, z0, z1)))
    cands.sort(key=lambda c: (c[2], -c[1]))  # most closed, then most frames
    return cands[0][0]


def turn_direction(heading_rate) -> str:
    """Left/right for a signed rate of change of heading atan2(z, x) in GT7's world frame.

    GT7's world is right-handed with y UP, so seen from above +z is 90 deg CLOCKWISE
    of +x (z = x cross y): an INCREASING atan2(z, x) is a RIGHT turn -- the mirror of
    the textbook x-right/y-up plane. Verified on real laps (tests/test_direction.py):
    the inner (more-steered) front wheel and GT7's angular_velocity_y (negative in
    right-handers) agree, and Suzuka T1 / Spa La Source come out right.
    """
    return "right" if heading_rate > 0 else "left"


def _smooth(a, w=9):
    k = np.ones(w) / w
    return np.convolve(np.concatenate([a[-w:], a, a[:w]]), k, mode="same")[w:-w]


def _classify(R, ang):
    if R < 45 and ang > 120:
        return "hairpin"
    if R < 120 and ang > 60:
        return "tight"
    if R >= 300 and ang < 45:
        return "kink/sweeper"
    if ang >= 45:
        return "medium"
    return "sweeper"


def detect_corners(rows):
    """Detect corners on the selected flying lap within `rows`.

    `rows` is a list of dict rows (as produced by csv.DictReader) with at least:
    pos_x, pos_z, vel_x, vel_z, car_speed, throttle, brake, gear, rot_roll,
    current_lap, package_id, in_race.

    Returns a list of Corner dicts, ordered by track position (turn 1..N).
    """
    idx = select_lap(rows)

    px = _col(rows, "pos_x")
    pz = _col(rows, "pos_z")
    vx = _col(rows, "vel_x")
    vz = _col(rows, "vel_z")
    spd = _col(rows, "car_speed") / 3.6     # km/h -> m/s
    thr = _col(rows, "throttle")
    brk = _col(rows, "brake")
    gear = _col(rows, "gear", int)
    roll = _col(rows, "rot_roll")

    # --- selected lap fields ---
    X, Z = px[idx], pz[idx]
    SP, TH, BR, GR = spd[idx], thr[idx], brk[idx], gear[idx]
    ROLL = roll[idx]
    n = len(idx)

    # CURVATURE from the VELOCITY-VECTOR HEADING RATE (horizontal plane), NOT angular_velocity_y:
    # High Speed Ring T1 is banked (roll ~6deg); body-frame yaw != horizontal heading rate there,
    # which makes speed/yaw blow up to unphysical g. Velocity heading is immune to banking/roll.
    VX, VZ = vx[idx], vz[idx]
    head = np.unwrap(np.arctan2(VZ, VX))
    YR = np.gradient(head) * 60.0        # heading rate (rad/s) at 60 Hz

    YRs = _smooth(YR, 11)
    SPs = _smooth(SP, 9)

    # NOTE: this is GAME telemetry. VGT cars (e.g. Dodge SRT Tomahawk X) exceed 600 km/h and pull
    # very high downforce-assisted lateral g. High speed / high g are VALID, never treated as corrupt.

    # noise floor from near-straight samples
    straight = np.abs(YRs) < 0.05
    floor = np.median(np.abs(YRs[straight])) if straight.any() else 0.02
    THR_YAW = max(0.045, 6 * floor)   # rad/s onset for "in a corner"

    # SIGN-AWARE segmentation: a corner is a run of consistent turn DIRECTION above threshold.
    # A heading-rate sign flip (right<->left) always ends one corner and starts the next, so a
    # chicane's two opposite curves become two corners and one curve never yields two apexes.
    sgn = np.where(YRs > THR_YAW, 1, np.where(YRs < -THR_YAW, -1, 0))
    raw = []
    a = None
    cs = 0
    last = 0
    for i in range(n):
        s = sgn[i]
        if s == 0:
            if a is not None:
                raw.append((a, last, cs))
                a = None
                cs = 0
        elif a is None:
            a = i
            cs = s
            last = i
        elif s == cs:
            last = i
        else:                       # sign flip -> new corner
            raw.append((a, last, cs))
            a = i
            cs = s
            last = i
    if a is not None:
        raw.append((a, last, cs))

    # drop blip runs (tiny integrated angle) so a brief opposite-sign wobble inside a long
    # corner doesn't split it; then merge SAME-sign neighbours across the removed blips/short gaps.
    # Never merge across a surviving sign flip (that would fuse a real chicane back into one).
    DT = 1 / 60.0
    MIN_BLIP_DEG = 18.0      # a sign-run below this is a wobble, not its own corner
    MERGE_SAMP = 90         # same-sign runs within this many samples are one corner

    def run_deg(s, e):
        return abs(np.sum(YR[s:e + 1]) * DT) * 180 / math.pi

    strong = [(s, e, sg) for (s, e, sg) in raw if run_deg(s, e) >= MIN_BLIP_DEG]
    segs = []
    for (s, e, sg) in strong:
        if segs and segs[-1][2] == sg and s - segs[-1][1] <= MERGE_SAMP:
            segs[-1] = (segs[-1][0], e, sg)
        else:
            segs.append((s, e, sg))
    segs = [(s, e) for (s, e, _) in segs]

    dt = 1 / 60.0
    corners = []
    for (a, b) in segs:
        seg = slice(a, b + 1)
        ang = math.degrees(abs(np.sum(YR[seg]) * dt))          # integrated heading change
        if ang < 20:
            continue
        # apex = where the direction change is HALF done (heading-midpoint), i.e. the centre of the
        # turn / geometric clipping point -- NOT max instantaneous curvature (which spikes at entry).
        cum = np.cumsum(YR[a:b + 1])
        tot = cum[-1]
        ai = a + int(np.argmin(np.abs(cum - tot / 2.0)))
        # radius: min over the segment core (representative corner tightness at the apex)
        core = slice(max(a, ai - 15), min(b + 1, ai + 16))
        Rapex = float(np.min(SPs[core] / np.maximum(np.abs(YRs[core]), 1e-3)))
        direction = turn_direction(YRs[ai])
        # ---- 4-phase ----
        # braking zone: last brake-onset (>8%) before apex within a lookback
        look = range(max(a - 90, 0), ai)
        braking = next((j for j in look if BR[j] > 8), None)
        # turn-in: first yaw past threshold from segment start
        turnin = a
        # exit: first strong throttle (>60%) after apex, else where yaw drops below threshold
        ex = next((j for j in range(ai, min(b + 120, n)) if TH[j] > 60), None)
        if ex is None:
            ex = b

        def w(j):
            return [round(float(X[j]), 2), round(float(Z[j]), 2)]

        corners.append({
            "apex_world": w(ai),
            "radius_m": round(float(Rapex), 1),
            "direction": direction,
            "heading_change_deg": round(ang, 1),
            "apex_speed_kmh": round(float(SPs[ai] * 3.6), 1),
            "lat_accel_g": round(float(SPs[ai] * abs(YRs[ai]) / 9.81), 2),  # game+downforce: high g is valid
            "length_m": round(float(np.sum(np.hypot(np.diff(X[seg]), np.diff(Z[seg])))), 0),
            "roll_deg_at_apex": round(float(math.degrees(ROLL[ai])), 1),
            "banked": bool(abs(ROLL[ai]) > 0.04),
            "phases": {
                "braking_zone": (w(braking) if braking is not None else None),
                "turn_in": w(turnin),
                "apex": w(ai),
                "exit": w(ex),
            },
            "gear_at_apex": int(GR[ai]),
            "source": "kinematic",
        })

    for i, c in enumerate(corners, 1):
        c["turn"] = i
        c["type"] = _classify(c["radius_m"], c["heading_change_deg"])

    return corners


def corners_from_stored(turns, affine):
    """Fallback corners from geometry.json stored turn labels (px) -> world.

    `turns` is a list of dicts with at least `turn`, `x`, `y` (px). Each stored
    turn location may be a hand-placed label, not a true telemetry apex, so
    results are flagged `apex_is_label=True`.
    """
    out = []
    for t in turns:
        x, z = affine.px_to_world(float(t["x"]), float(t["y"]))
        out.append({"turn": t.get("turn"), "apex_world": [x, z], "source": "stored",
                    "apex_is_label": True, "radius_m": None, "direction": None,
                    "heading_change_deg": None, "type": None})
    return out


def corners_from_outline(outline_px, affine):
    """Fallback corners from coarse SVG-outline curvature (position-only heading change).

    `outline_px` is a closed polyline of (px, py) points. No telemetry required.

    `direction` is always None (unknown): the SVG path's drawing order is not the
    driving direction (forward and reverse twins share one outline), so the sign of
    its curvature says nothing about left vs right. An unverified direction must
    never be stored as fact; provenance still records these corners as
    `outline_curvature`. For the same reason `heading_change_deg` is stored as an
    unsigned magnitude (its sign would encode that drawing order), matching the
    driven-lap and derived-reverse corners, which are unsigned too.
    """
    if len(outline_px) < 4:
        return []
    world = [list(affine.px_to_world(px, py)) for px, py in outline_px]
    P = np.asarray(world, float)
    out = []
    for i in range(len(P)):
        a, b, c = P[i - 1], P[i], P[(i + 1) % len(P)]
        h0 = math.atan2(b[1] - a[1], b[0] - a[0])
        h1 = math.atan2(c[1] - b[1], c[0] - b[0])
        dh = math.degrees(math.atan2(math.sin(h1 - h0), math.cos(h1 - h0)))
        if abs(dh) >= 25:
            out.append({"turn": len(out) + 1, "apex_world": [float(b[0]), float(b[1])],
                        "source": "outline_curvature", "apex_is_label": False,
                        "radius_m": None, "direction": None,
                        "heading_change_deg": round(abs(dh), 1), "type": None})
    return out
