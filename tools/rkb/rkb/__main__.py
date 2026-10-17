"""CLI entry point: `python -m rkb` builds the full RKB from the default
repo/data paths. Course/detect externals are fetched by `fetch_sources.sh`
(T16); until they exist, build_all degrades gracefully (kb-only join="fuzzy"
records) rather than crashing."""
import csv
import os

from . import capture as capture_mod
from .build import build_all

# gt7 repo root is 4 parents up from this file:
# .../gt7/backend/tools/rkb/rkb/__main__.py -> .../gt7
_GT7_ROOT = os.path.dirname(
    os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
)
KB_DIR = os.path.join(_GT7_ROOT, "kb", "track-layouts-svg")
COURSE_CSV = "data/external/course.csv"
DETECT_CSV = "data/external/gt7trackdetect.csv"
OUT_DIR = "out"

# HSR Reverse has no stored `turns` in its geometry.json (outline-only), but a clean
# flying-lap telemetry fixture is committed, so wire it through the kinematic detector
# instead of falling back to outline corners.
_HSR_LAP_CSV = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "tests", "fixtures", "hsr_lap.csv")

# rkb slug -> VictoryDash replay dir name (under <gt7 root>/supporting-data/victorydash/).
# VictoryDash replays carry authoritative world-coordinate boundaries + a real driven lap,
# so these slugs get their affine/boundary/width/corners overridden from the replay instead
# of the generic KB-only pipeline (see build_track's vd_dir path).
_VD_IDENTITY_MAP = {
    "interlagos": "interlagos-time-trial-a-tapai",
    "spa-francorchamps": "spa-time-trial-antonio-santos",
    "autopolis-shortcut": "week-27-daily-a-sirvia",
    "mount-panorama": "week-27-daily-c-c-birtles",
    "deep-forest-2": "week-27-daily-c-k-drumont",
    "sardegna-road-a": "week-28-daily-a-sardegna",
    "watkins-glen-long": "week-28-daily-c-watkins-glen",
    "willow-springs-big-willow": "week-29-daily-a-a-ruizz",
    "nurburgring-gp": "week-29-daily-b-a-haydar",
    "suzuka": "week-29-daily-c-ryun",
}

_VD_ROOT = os.path.join(_GT7_ROOT, "supporting-data", "victorydash")

# rkb slug -> our own raw pcap packet capture (under
# <gt7 root>/supporting-data/raw-packet-captures/). These are OUR captures --
# authoritative driven world coordinates (like VictoryDash telemetry, but with
# no separate boundary file) -- decoded via pcap2csv and fed through
# rkb.capture. Resolves a needs-capture track's affine + kinematic corners.
_RAW_CAPTURE_ROOT = os.path.join(_GT7_ROOT, "supporting-data", "raw-packet-captures")
_CAPTURE_MAP = {
    "grand-valley-south-2": os.path.join(
        _RAW_CAPTURE_ROOT,
        "grand-valley-south-reverse-4laps-1stRight-2ndLeft_with_pit_and_track_width-3rdLeft_with_pit_4thCentre.pcapng",
    ),
    "brands-hatch-indy": os.path.join(_RAW_CAPTURE_ROOT, "brand-hatch-indy-3laps-3rd-clean.pcapng"),
    "special-stage-route-x": os.path.join(_RAW_CAPTURE_ROOT, "special-route-x-1lap.pcapng"),
}

# Captures taken on current GT7 versions (in_race never set; rkb.corners.on_track
# gates them instead) that mix a live stint with menus / a race replay / a replay
# rewind. Only the LIVE row range [start, end) of the decoded CSV is ingested, so
# the affine bbox and the corner lap come from our own driving, never a replay.
_CAPTURE_DIR = os.path.join(_GT7_ROOT, "captures")
_CAPTURE_ROW_RANGES = {
    # Red Bull Ring (full layout), WTC700: rows 0-18854 = live stint, laps 1-3
    # complete (92.192 / 95.525 / 95.043 s); row 18855 on is pit/menu, grid, a GTWS
    # race replay and its rewind.
    "red-bull-ring": (os.path.join(_CAPTURE_DIR, "redbull-ring-20261017-WTC700-GTWS.pcapng"), 0, 18855),
    # The slugs below were identified from the data, not the file names: driven lap
    # distance vs tracks.length_m, the bbox-fit residual of the driven lap against each
    # candidate's SVG outline, and direction (the lap's visit order of the SVG's turn /
    # sector labels from the S/F rollover, plus the official map's direction arrow).
    #
    # Daytona Tri-Oval (not the road course): lap 1 drove 4025 m (tri-oval 4023, road
    # 5729); outline residual 3.1 m vs 72 m. Rows 3535-6944 = lap 1 only, a complete
    # flying lap; lap 2 holds the pause/home/resume (4465 frozen frames).
    "daytona-tri-oval": (
        os.path.join(_RAW_CAPTURE_ROOT, "daytona-2laps-pause-home-resume.pcapng"), 3535, 6945
    ),
    # Dragon Trail Gardens, FORWARD: laps drove 4276-4299 m (Gardens 4352, Seaside 5209,
    # Seaside bbox 44% anisotropic); the lap visits Gardens' turn labels 3,10,11,12,13,14
    # in increasing order (Gardens Reverse's 10..14 in decreasing order). Rows 305-19217
    # = laps 1-3, all complete and pause-free.
    "dragon-trail-gardens": (os.path.join(_RAW_CAPTURE_ROOT, "dragontail-3laps.pcapng"), 305, 19218),
    # Brands Hatch GP: 3876 m/lap (GP 3946, Indy 1944); residual 6.5 m vs 125 m. Rows
    # 404-16818 = laps 1-3 (lap 0 opens with a 270 m position jump).
    "brands-hatch-gp": (os.path.join(_RAW_CAPTURE_ROOT, "brands-hatch-grand-prix-3laps.pcapng"), 404, 16819),
    # 24 Heures du Mans WITH the Mulsanne chicanes: length (13549 m vs 13629 / 13567) and
    # outline residual (37.8 vs 38.2 m) cannot separate the two layouts, but the lap has
    # two left-right-left chicanes at 83-104 km/h exactly at the chicane layout's turn
    # labels 10-14 on the Mulsanne straight, which the no-chicane layout does not have.
    # Rows 25631-38240 = lap 3, a complete lap with no frozen (paused) frames.
    "24-heures-du-mans": (os.path.join(_RAW_CAPTURE_ROOT, "le-mans-5laps.pcapng"), 25631, 38241),
    # High Speed Ring FORWARD, despite the file name: on the main straight the car moves
    # toward -x (the forward map's arrow points left; the reverse map's points right), and
    # the lap's winding is the opposite of tests/fixtures/hsr_lap.csv, which IS the reverse
    # layout (high-speed-ring-2). Rows 303-24915 = laps 1-6; lap 7 on has pauses and a
    # 165 m reset jump.
    "high-speed-ring": (
        os.path.join(_RAW_CAPTURE_ROOT, "high-speed-ring-reverse-layout-20laps.pcapng"), 303, 24916
    ),
}


def _load_capture_row_range_csv_map() -> dict:
    out = {}
    for slug, (pcap, start, end) in _CAPTURE_ROW_RANGES.items():
        if not os.path.isfile(pcap):
            continue
        try:
            with open(capture_mod.decode_capture(pcap)) as f:
                rows = list(csv.DictReader(f))[start:end]
            csv_path = os.path.join(OUT_DIR, "capture-csv", f"{slug}.csv")
            capture_mod._write_rows_csv(rows, csv_path)
            out[slug] = csv_path
        except Exception as exc:  # noqa: BLE001 - degrade gracefully when the capture is missing/bad
            print(f"skipping {slug} capture: {exc}")
    return out


# Sardegna: ONE capture drove all 6 road layouts (a/a2/b/b2/c/c2) back-to-back with
# no clean lap-counter separation between them -- the lap counter does not reset at
# a scenery-select boundary, only the car's position teleports. Split by position
# discontinuity (rkb.capture.split_capture_by_jumps), match each resulting segment to
# a layout by nearest bbox center, then split each center's forward/reverse pair by
# traversal winding (rkb.capture.segment_winding_sign).
#
# Candidate bbox centers (world metres, from rkb.db / the existing bbox_from_driven_trace
# tier for these slugs).
_SARDEGNA_MULTI_LAYOUT_PCAP = os.path.join(
    _RAW_CAPTURE_ROOT, "sardegna-layout-a-b-c-forward-and-reverse-6laps-lap-3-width.pcapng"
)
_SARDEGNA_SLUG_BBOXES = {
    "sardegna-road-a": (-8.0, 6.0),
    "sardegna-road-a2": (-8.0, 6.0),
    "sardegna-road-b": (-172.0, 146.0),
    "sardegna-road-b2": (-172.0, 146.0),
    "sardegna-road-c": (-130.0, 311.0),
    "sardegna-road-c2": (-130.0, 311.0),
}
# Winding-sign calibration: sardegna-road-a is ALREADY resolved via VictoryDash
# (see _VD_IDENTITY_MAP below); its VictoryDash driven lap has signed-area sign +1
# (computed once from out/vd-csv/week-28-daily-a-sardegna.csv's clean lap). The
# segment in each a/b/c pair whose winding matches +1 is the forward layout
# (no "2" suffix); the opposite sign is the reverse ("2"-suffixed) layout. This
# convention is applied uniformly to b/b2 and c/c2 -- verified after build by
# checking the two members of each pair produce distinct, non-degenerate corner
# sets (see .superpowers/sdd/sardegna-ingest-report.md).
_SARDEGNA_FORWARD_SIGN = {
    "sardegna-road-a": 1,
    "sardegna-road-a2": -1,
    "sardegna-road-b": 1,
    "sardegna-road-b2": -1,
    "sardegna-road-c": 1,
    "sardegna-road-c2": -1,
}


def _load_sardegna_capture_csv_map() -> dict:
    if not os.path.isfile(_SARDEGNA_MULTI_LAYOUT_PCAP):
        return {}
    try:
        return capture_mod.capture_csvs_for_multi_layout(
            _SARDEGNA_MULTI_LAYOUT_PCAP, _SARDEGNA_SLUG_BBOXES, _SARDEGNA_FORWARD_SIGN
        )
    except Exception as exc:  # noqa: BLE001 - degrade gracefully when the capture is missing/bad
        print(f"skipping sardegna multi-layout capture: {exc}")
        return {}


def _load_telemetry_map() -> dict:
    telemetry_map = {}
    if os.path.exists(_HSR_LAP_CSV):
        with open(_HSR_LAP_CSV) as f:
            telemetry_map["high-speed-ring-2"] = list(csv.DictReader(f))
    return telemetry_map


def _load_vd_map() -> dict:
    vd_map = {}
    for slug, vd_slug in _VD_IDENTITY_MAP.items():
        vd_dir = os.path.join(_VD_ROOT, vd_slug)
        if os.path.isdir(vd_dir):
            vd_map[slug] = vd_dir
    return vd_map


def _load_capture_map() -> dict:
    return {slug: p for slug, p in _CAPTURE_MAP.items() if os.path.isfile(p)}


# Slugs with a deliberate edge-hugging "width lap" capture (see the Track Width from
# Surface Type Design spec, CHOSEN SCOPE): grand-valley-south-2's own capture, plus the
# 6 Sardegna road layouts split out of the shared multi-layout pcap. sardegna-road-a
# uniquely also has a VictoryDash true width -- it is the calibration anchor (see
# rkb.build._WIDTH_CALIBRATION_ANCHOR_SLUG / rkb.width_calib) -- its own width-lap
# capture is used to CROSS-CHECK the (still-authoritative) VictoryDash width, not
# override it.
_WIDTH_LAP_SLUGS = {
    "grand-valley-south-2",
    "sardegna-road-a",
    "sardegna-road-a2",
    "sardegna-road-b",
    "sardegna-road-b2",
    "sardegna-road-c",
    "sardegna-road-c2",
}


def main() -> None:
    build_all(
        kb_dir=KB_DIR,
        course_csv=COURSE_CSV,
        detect_csv=DETECT_CSV,
        telemetry_map=_load_telemetry_map(),
        vd_map=_load_vd_map(),
        out_dir=OUT_DIR,
        capture_map=_load_capture_map(),
        capture_csv_overrides={**_load_sardegna_capture_csv_map(), **_load_capture_row_range_csv_map()},
        width_lap_slugs=_WIDTH_LAP_SLUGS,
    )


if __name__ == "__main__":
    main()
