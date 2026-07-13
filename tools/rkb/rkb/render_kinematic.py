#!/usr/bin/env python3
"""Clean kinematic corner render: input-coloured driven line, small numbers BESIDE the
track (official numbering via nearest-official-corner), sector labels + boundary ticks.
Marker coords are what the RKB will store, so keep the on-track marks minimal."""
import csv, json, math, sys
import numpy as np

KJSON, CSVF, OUT = sys.argv[1], sys.argv[2], sys.argv[3]

# inverse affine (SVG px -> world m), from the HSR overlay
SX, OX, SZ, OZ = 0.59828, 626.61, 0.59816, 554.88
def w(px, py): return ((px-OX)/SX, (py-OZ)/SZ)

# official corner label positions read from the reference overlay (SVG px) -> world
OFFICIAL_PX = {1:(1090,405), 2:(905,540), 3:(670,775), 4:(580,660), 5:(310,915), 6:(160,340)}
OFFICIAL = {k: w(*v) for k,v in OFFICIAL_PX.items()}
# sector name labels (SVG px) -> world ; S1 read from reference (right side)
SECT_PX = {"S1":(1050,520), "S2":(631,834), "S3":(90,402)}
SECT = {k: w(*v) for k,v in SECT_PX.items()}

d = json.load(open(KJSON))
rows = [r for r in csv.DictReader(open(CSVF)) if r["in_race"]=="true"]
px = np.array([float(r["pos_x"]) for r in rows]); pz = np.array([float(r["pos_z"]) for r in rows])
lap = np.array([int(r["current_lap"]) for r in rows]); pid = np.array([int(r["package_id"]) for r in rows])
thr = np.array([float(r["throttle"]) for r in rows]); brk = np.array([float(r["brake"]) for r in rows])
runs=[];cur=[]
for i in range(len(rows)):
    if i>0 and (lap[i]!=lap[i-1] or pid[i]-pid[i-1]>10 or pid[i]<pid[i-1]) and cur: runs.append(cur);cur=[]
    cur.append(i)
if cur:runs.append(cur)
best=min([r for r in runs if 2500<len(r)<7000], key=lambda r: abs(px[r[0]]-px[r[-1]])+abs(pz[r[0]]-pz[r[-1]]))
idx=np.array(best); X,Z,T,B = px[idx],pz[idx],thr[idx],brk[idx]

# --- assign each derived apex a unique official number (greedy nearest) ---
apexes = [(c, tuple(c["apex_world"])) for c in d["corners"]]
pairs = []
for ci,(c,a) in enumerate(apexes):
    for onum,op in OFFICIAL.items():
        pairs.append((math.hypot(a[0]-op[0], a[1]-op[1]), ci, onum))
pairs.sort()
usedC=set(); usedO=set(); assign={}
for dist,ci,onum in pairs:
    if ci in usedC or onum in usedO: continue
    assign[ci]=onum; usedC.add(ci); usedO.add(onum)

cx, cz = float(X.mean()), float(Z.mean())   # track centroid, to push labels outward
vbx,vbz = X.min()-120, Z.min()-120; vbw,vbh=(X.max()-X.min())+240,(Z.max()-Z.min())+240
sc=max(vbw,vbh)

segs=""
for i in range(len(X)-1):
    r=int(min(255,B[i]/100*255)); g=int(min(255,T[i]/100*255))
    col="#4a5568" if (r<40 and g<40) else f"rgb({max(r,50)},{max(g,50) if g>40 else 50},60)"
    segs+=f'<line x1="{X[i]:.1f}" y1="{Z[i]:.1f}" x2="{X[i+1]:.1f}" y2="{Z[i+1]:.1f}" stroke="{col}" stroke-width="{sc/280}"/>'

marks=""
for ci,(c,a) in enumerate(apexes):
    onum = assign.get(ci, c["turn"])
    ax,az=a
    # outward offset for the label
    dx,dz = ax-cx, az-cz; L=math.hypot(dx,dz) or 1; off=sc/16
    lx,ly = ax+dx/L*off, az+dz/L*off
    marks+=f'<circle cx="{ax}" cy="{az}" r="{sc/260}" fill="#fff"/>'
    marks+=f'<line x1="{ax}" y1="{az}" x2="{lx}" y2="{ly}" stroke="#888" stroke-width="{sc/900}"/>'
    marks+=f'<text x="{lx}" y="{ly}" text-anchor="middle" dominant-baseline="middle" font-family="monospace" font-weight="700" font-size="{sc/26}" fill="#fff">{onum}</text>'
    # per-corner details (radius/speed/g/gear/phases) live in the RKB JSON, not on the map

# sector boundary ticks: S1=1-2, S2=2-5, S3=5-6 -> boundaries at turns 2 and 5 (shared)
sect=""
for onum,lbl in [(2,"S1|S2"),(5,"S2|S3")]:
    op=OFFICIAL[onum]
    sect+=f'<circle cx="{op[0]:.0f}" cy="{op[1]:.0f}" r="{sc/120}" fill="none" stroke="#b14fff" stroke-width="{sc/500}" stroke-dasharray="{sc/120},{sc/240}"/>'
for name,p in SECT.items():
    sect+=f'<text x="{p[0]:.0f}" y="{p[1]:.0f}" text-anchor="middle" font-family="monospace" font-weight="700" font-size="{sc/24}" fill="#b14fff" font-style="italic">{name}</text>'

svg=f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="{vbx:.0f} {vbz:.0f} {vbw:.0f} {vbh:.0f}" width="1200" height="{round(1200*vbh/vbw)}">
<rect x="{vbx:.0f}" y="{vbz:.0f}" width="{vbw:.0f}" height="{vbh:.0f}" fill="#0b0e13"/>
{segs}{sect}{marks}
<text x="{vbx+40}" y="{vbz+50}" font-family="monospace" font-size="{sc/50}" fill="#8b949e">High Speed Ring Reverse | line: green=throttle red=brake grey=coast | numbers=official turns | S#|S#=sector split</text>
</svg>'''
open(OUT,"w").write(svg)
print("assignment (derived-order -> official):", {apexes[ci][0]["turn"]:assign[ci] for ci in assign})
print("wrote",OUT)
