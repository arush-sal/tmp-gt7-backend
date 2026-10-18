#!/usr/bin/env python3
"""
Kinematic corner derivation from FULL-FIELD GT7 telemetry (the real capture, not the
stripped golden). Corner radius/apex/direction come from measured yaw rate + speed
(NOT differentiated position); the 4-phase model comes from brake/throttle.

  radius R      = speed_ms / |yaw_rate|      (yaw_rate = angular_velocity_y, rad/s)
  lateral accel = speed_ms * |yaw_rate|      (== v^2 / R)
  apex          = max |yaw_rate| in the corner run
  braking zone  = brake onset before apex
  turn-in       = |yaw_rate| rising past the straight-line noise floor
  exit          = throttle reapplied / |yaw_rate| decaying after apex
"""
import csv, json, math, sys
import numpy as np

CSV = sys.argv[1]; OUT = sys.argv[2]
WANT = sys.argv[3] if len(sys.argv) > 3 else "auto"   # lap index within chosen run, or "auto"

rows = list(csv.DictReader(open(CSV)))
def col(name, cast=float):
    return np.array([cast(r[name]) for r in rows])

pid   = col("package_id", int)
lap   = col("current_lap", int)
inr   = np.array([r["in_race"] == "true" for r in rows])
px    = col("pos_x"); pz = col("pos_z")
vx    = col("vel_x"); vz = col("vel_z")
yawr  = col("angvel_y")            # yaw rate rad/s
spd   = col("car_speed") / 3.6     # km/h -> m/s
thr   = col("throttle"); brk = col("brake")
gear  = col("gear", int)

# --- segment into contiguous lap runs: break on lap change, pid gap, or not-in-race ---
runs = []; cur = []
for i in range(len(rows)):
    brk_seg = (not inr[i]) or (i > 0 and (lap[i] != lap[i-1] or pid[i] - pid[i-1] > 10 or pid[i] < pid[i-1]))
    if brk_seg and cur:
        runs.append(cur); cur = []
    if inr[i]:
        cur.append(i)
if cur: runs.append(cur)

def bbox(idx):
    return (px[idx].min(), px[idx].max(), pz[idx].min(), pz[idx].max())

# choose the clean HSR flying lap: closed loop, big x-span, plausible frame count
cands = []
for r in runs:
    idx = np.array(r)
    if len(idx) < 2500 or len(idx) > 45000: continue   # cap raised for long tracks (Nordschleife ~30k)
    x0,x1,z0,z1 = bbox(idx)
    closed = math.hypot(px[idx[0]]-px[idx[-1]], pz[idx[0]]-pz[idx[-1]])
    span = (x1-x0) + (z1-z0)
    if span < 400: continue   # reject stationary/pit clusters (spawn idle etc.) — not a driven lap
    cands.append((idx, len(idx), closed, span, (x0,x1,z0,z1)))
cands.sort(key=lambda c: (c[2], -c[1]))  # most closed, then most frames
print(f"runs: {len(runs)}  lap-candidates: {len(cands)}")
for k,(idx,n,cl,sp,bb) in enumerate(cands[:6]):
    print(f"  cand{k}: {n} frames  loop-gap={cl:.0f}m  x[{bb[0]:.0f},{bb[1]:.0f}] z[{bb[2]:.0f},{bb[3]:.0f}]")
sel = cands[0][0] if WANT == "auto" else cands[int(WANT)][0]
idx = sel

# --- selected lap fields ---
X, Z = px[idx], pz[idx]
VX, VZ = vx[idx], vz[idx]
SP, TH, BR, GR = spd[idx], thr[idx], brk[idx], gear[idx]
BODYYAW, ROLL = yawr[idx], col("rot_roll")[idx]
n = len(idx)

# CURVATURE from the VELOCITY-VECTOR HEADING RATE (horizontal plane), NOT angular_velocity_y:
# High Speed Ring T1 is banked (roll ~6deg); body-frame yaw != horizontal heading rate there,
# which makes speed/yaw blow up to unphysical g. Velocity heading is immune to banking/roll.
head = np.unwrap(np.arctan2(VZ, VX))
YR = np.gradient(head) * 60.0        # heading rate (rad/s) at 60 Hz

def smooth(a, w=9):
    k = np.ones(w)/w
    return np.convolve(np.concatenate([a[-w:], a, a[:w]]), k, mode="same")[w:-w]

YRs = smooth(YR, 11)
SPs = smooth(SP, 9)

# NOTE: this is GAME telemetry. VGT cars (e.g. Dodge SRT Tomahawk X) exceed 600 km/h and pull
# very high downforce-assisted lateral g. High speed / high g are VALID, never treated as corrupt.

# noise floor from near-straight samples
straight = np.abs(YRs) < 0.05
floor = np.median(np.abs(YRs[straight])) if straight.any() else 0.02
THR_YAW = max(0.045, 6*floor)   # rad/s onset for "in a corner"
print(f"heading-rate noise floor={floor:.4f} rad/s -> corner threshold={THR_YAW:.4f}")

# SIGN-AWARE segmentation: a corner is a run of consistent turn DIRECTION above threshold.
# A heading-rate sign flip (right<->left) always ends one corner and starts the next, so a
# chicane's two opposite curves become two corners and one curve never yields two apexes.
sgn = np.where(YRs > THR_YAW, 1, np.where(YRs < -THR_YAW, -1, 0))
raw = []; a = None; cs = 0; last = 0
for i in range(n):
    s = sgn[i]
    if s == 0:
        if a is not None: raw.append((a, last, cs)); a = None; cs = 0
    elif a is None:
        a = i; cs = s; last = i
    elif s == cs:
        last = i
    else:                       # sign flip -> new corner
        raw.append((a, last, cs)); a = i; cs = s; last = i
if a is not None: raw.append((a, last, cs))

# drop blip runs (tiny integrated angle) so a brief opposite-sign wobble inside a long
# corner doesn't split it; then merge SAME-sign neighbours across the removed blips/short gaps.
# Never merge across a surviving sign flip (that would fuse a real chicane back into one).
DT = 1/60.0
MIN_BLIP_DEG = 18.0      # a sign-run below this is a wobble, not its own corner
MERGE_SAMP = 90         # same-sign runs within this many samples are one corner
def run_deg(s, e): return abs(np.sum(YR[s:e+1]) * DT) * 180/math.pi
strong = [(s, e, sg) for (s, e, sg) in raw if run_deg(s, e) >= MIN_BLIP_DEG]
segs = []
for (s, e, sg) in strong:
    if segs and segs[-1][2] == sg and s - segs[-1][1] <= MERGE_SAMP:
        segs[-1] = (segs[-1][0], e, sg)
    else:
        segs.append((s, e, sg))
segs = [(s, e) for (s, e, _) in segs]

dt = 1/60.0
corners = []
for (a, b) in segs:
    seg = slice(a, b+1)
    ang = math.degrees(abs(np.sum(YR[seg]) * dt))          # integrated heading change
    if ang < 20: continue
    # apex = where the direction change is HALF done (heading-midpoint), i.e. the centre of the
    # turn / geometric clipping point -- NOT max instantaneous curvature (which spikes at entry).
    cum = np.cumsum(YR[a:b+1]); tot = cum[-1]
    ai = a + int(np.argmin(np.abs(cum - tot/2.0)))
    # radius: min over the segment core (representative corner tightness at the apex)
    core = slice(max(a, ai-15), min(b+1, ai+16))
    Rapex = float(np.min(SPs[core] / np.maximum(np.abs(YRs[core]), 1e-3)))
    direction = "right" if YRs[ai] > 0 else "left"   # GT7 y-up world: +heading rate = RIGHT (see rkb.corners.turn_direction)
    # ---- 4-phase ----
    # braking zone: last brake-onset (>8%) before apex within a lookback
    look = range(max(a-90, 0), ai)
    braking = next((j for j in look if BR[j] > 8), None)
    # turn-in: first yaw past threshold from segment start
    turnin = a
    # exit: first strong throttle (>60%) after apex, else where yaw drops below threshold
    ex = next((j for j in range(ai, min(b+120, n)) if TH[j] > 60), None)
    if ex is None: ex = b
    def w(j): return [round(float(X[j]),2), round(float(Z[j]),2)]
    corners.append({
        "apex_world": w(ai),
        "radius_m": round(float(Rapex),1),
        "direction": direction,
        "heading_change_deg": round(ang,1),
        "apex_speed_kmh": round(float(SPs[ai]*3.6),1),
        "lat_accel_g": round(float(SPs[ai]*abs(YRs[ai])/9.81),2),  # game+downforce: high g is valid
        "length_m": round(float(np.sum(np.hypot(np.diff(X[seg]), np.diff(Z[seg])))),0),
        "roll_deg_at_apex": round(float(math.degrees(ROLL[ai])),1),
        "banked": bool(abs(ROLL[ai]) > 0.04),
        "phases": {
            "braking_zone": (w(braking) if braking is not None else None),
            "turn_in": w(turnin),
            "apex": w(ai),
            "exit": w(ex),
        },
        "gear_at_apex": int(GR[ai]),
    })

def classify(R, ang):
    if R < 45 and ang > 120: return "hairpin"
    if R < 120 and ang > 60: return "tight"
    if R >= 300 and ang < 45: return "kink/sweeper"
    if ang >= 45: return "medium"
    return "sweeper"
for i,c in enumerate(corners,1):
    c["turn"] = i
    c["type"] = classify(c["radius_m"], c["heading_change_deg"])

out = {
    "source_csv": CSV, "frames": int(n),
    "units": "metres / km/h / rad/s (GT7 world; yaw_rate=angular_velocity_y)",
    "method": "radius=speed/yaw_rate; apex=max|yaw_rate|; phases from brake/throttle",
    "corners": corners,
}
json.dump(out, open(OUT,"w"), indent=2)
print(f"\ncorners found: {len(corners)}")
for c in corners:
    ph = c["phases"]; bz = "brk" if ph["braking_zone"] else "no-brk"
    print(f"  T{c['turn']}: {c['type']:12} R={c['radius_m']:6.1f}m {c['direction']:5} dH={c['heading_change_deg']:+6.1f} "
          f"vApex={c['apex_speed_kmh']:5.1f}km/h {c['lat_accel_g']:.2f}g g{c['gear_at_apex']} {bz} apex=({c['apex_world'][0]:.0f},{c['apex_world'][1]:.0f})")
