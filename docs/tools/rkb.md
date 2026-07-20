# rkb — Track Reference Knowledge Base pipeline

`rkb` is an offline Python pipeline that generates the **Track Reference Knowledge Base (RKB)**: one
canonical record per GT7 track, with world-metre geometry, corners, sectors, track width, a
confidence gate, and provenance. Its main output is the SQLite database (`rkb.db`) that the backend
serves through its [track API](../api.md).

It lives inside the Go module at `tools/rkb/` so the `pcap2csv` decoder can reuse the backend's
packet-decode code, while the rest of the pipeline is a self-contained Python package (`rkb/`).

## Prerequisites

- Python with the pipeline's virtual environment at `tools/rkb/.venv`.
- Dependencies: `numpy>=2`, `scipy>=1.13` (`tools/rkb/requirements.txt`).

## How to run

```bash
cd tools/rkb
./.venv/bin/python -m rkb
```

This runs the full pipeline (`build_all`) with the repo's default paths and writes everything under
`tools/rkb/out/`.

> The `README.md` in `tools/rkb/` refers to `python -m rkb.build`; the working entrypoint is
> `python -m rkb` (which calls `build_all`).

The pipeline **degrades gracefully**: if an external source or a capture is missing, it logs a skip
line and continues, producing lower-tier records rather than failing.

## Inputs

| Input | Location | Role |
|---|---|---|
| KB track SVGs | `<gt7-root>/kb/track-layouts-svg` | Track outlines and sector-turn ranges. |
| `course.csv` | `data/external/course.csv` | Track identities / metadata. |
| `gt7trackdetect.csv` | `data/external/gt7trackdetect.csv` | Track-detection reference. |
| VictoryDash replays | `<gt7-root>/supporting-data/victorydash/` | Authoritative world-coordinate boundaries + a real driven lap (10 mapped tracks). |
| Raw pcap captures | `<gt7-root>/supporting-data/raw-packet-captures/` | Our own driven laps for specific tracks (incl. Sardegna's multi-layout capture). |
| Width-lap captures | (subset of the pcaps) | Edge-hugging laps used to measure track width. |

`data/external/` and `out/` are git-ignored working directories.

### Fetching external sources

`tools/rkb/fetch_sources.sh` populates `data/external/` (requires an authenticated `gh` CLI):

- `course.csv` from `ddm999/gt7info`
- `gt7trackdetect.csv` from `Bornhall/gt7telemetry`
- track dumps from `vthinsel/GT7Tracks`

## Outputs

Written under `tools/rkb/out/`:

| Output | What it is |
|---|---|
| `out/rkb.db` | The normalized SQLite RKB — the backend's source of truth. |
| `out/rkb-index.csv` | Flat index (the track-picker rows). |
| `out/rkb/<slug>.rkb.json` | Full canonical record per track (human-readable). |
| `out/capture-csv/`, `out/vd-csv/` | Decoded CSVs from pcap / VictoryDash captures. |

Serving priority is **DB > JSON > CSV**; the JSON records are the human-readable view of what the DB
holds.

### Installing the DB for the backend

The backend reads `$HOME/.garage/rkb.db`. After a rebuild, copy the generated database into place:

```bash
mkdir -p ~/.garage
cp tools/rkb/out/rkb.db ~/.garage/rkb.db
```

## Package modules

The `rkb/` package is organized by concern:

| Module | Role |
|---|---|
| `sources.py` | Load KB SVGs, `course.csv`, `gt7trackdetect.csv`. |
| `ids.py` | Reconcile KB / course / detect track identities. |
| `names.py` | Canonicalize track names (accents, filler words, reverse -> rev). |
| `affine.py` | Choose the SVG-pixel <-> world-metre transform. |
| `boundary.py` | Build a boundary from an SVG outline or VictoryDash edges. |
| `corners.py` | Derive kinematic corners from telemetry (heading-rate, 4-phase model). |
| `sectors.py` | Parse KB sector-turn ranges into sector records. |
| `width.py` | Track width from measured boundaries or SVG stroke (3-40 m band). |
| `width_surface.py` | Scalar width from a Format-C "width lap" (per-wheel surface codes). |
| `width_calib.py` | Per-car half-track width calibration from the Sardegna anchor. |
| `capture.py` | Ingest our own pcap captures (driven trace, no measured boundary). |
| `victorydash.py` | Ingest VictoryDash replays (true edges + driven lap). |
| `reverse.py` | Derive a reverse-layout record from its resolved forward twin. |
| `gate.py` | Confidence gate (corner count, coordinate usability, boundary fidelity). |
| `schema.py` | RKB record schema + validation. |
| `db.py` | Build the normalized SQLite RKB. |
| `index.py` | Write `rkb-index.csv`. |
| `build.py` | Orchestrator: wires all of the above into one record per track + the batch driver. |
| `derive_kinematic.py` | Earlier standalone kinematic-corner prototype. |
| `render.py`, `render_kinematic.py` | SVG/PNG visualizations of a record (derived, disposable). |
| `validate.py` | Corpus validation harness (usability tiers, leave-one-out affine sanity). |
| `gen-trace-svg.mjs` | Node helper: render a driven path CSV to a world-coordinate SVG. |

## JSON is the source of truth (for review)

The per-track JSON record is the canonical human-readable output. Rendered PNG/SVG images are
derived, disposable views — never hand-edit an image or treat it as authoritative. If a rendering
looks wrong, fix the JSON (or the code that derives it) and regenerate.
