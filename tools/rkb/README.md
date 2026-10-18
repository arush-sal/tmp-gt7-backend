# rkb — offline Track RKB generation

Offline Python pipeline that generates per-track "RKB" (track reference/kinematic
baseline) records for GT7 tracks: ingesting captured telemetry, deriving kinematic
data (position/heading/curvature along the racing line), and rendering both a
canonical JSON record and supporting visualizations (PNG/SVG) per track.

This tooling lives inside the backend Go module
(`github.com/arush-sal/garage/backend`) at `backend/tools/rkb/` so the
`pcap2csv` decoder can reuse the backend's packet-decode/decrypt code directly,
while the rest of the pipeline is a self-contained Python package (`rkb/`).

## How to run

```bash
cd backend/tools/rkb
./.venv/bin/python -m rkb
```

(`rkb/__main__.py` is the pipeline entry point; it calls `rkb.build.build_all`
with the default data paths.)

To rebuild the `pcap2csv` decoder from the backend module root:

```bash
cd backend
go build ./tools/rkb/pcap2csv
```

## Data sources

| Source | Format | Produced by | Consumed by |
|---|---|---|---|
| `.pcapng` capture | raw GT7 UDP telemetry packets | PS5 + packet capture | `pcap2csv` |
| CSV (decoded telemetry) | full-field CSV per packet | `pcap2csv` | `rkb.derive_kinematic` |
| kinematic JSON | derived per-track kinematic record | `derive_kinematic.py` | `render_kinematic.py`, `gen-trace-svg.mjs` |
| kinematic PNG/SVG | rendered visualization | `render_kinematic.py`, `gen-trace-svg.mjs` | human review |

`data/external/` and `out/` are git-ignored working directories for raw
captures/reference data and generated artifacts, respectively.

## JSON is the source of truth

The per-track kinematic JSON record is the canonical output of this pipeline.
Rendered PNG/SVG visualizations are derived, disposable views of that JSON —
never hand-edit an image or treat it as authoritative. If a rendering looks
wrong, fix the JSON (or the code that derives it), then regenerate the
rendering.
