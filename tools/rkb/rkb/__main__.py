"""CLI entry point: `python -m rkb` builds the full RKB from the default
repo/data paths. Course/detect externals are fetched by `fetch_sources.sh`
(T16); until they exist, build_all degrades gracefully (kb-only join="fuzzy"
records) rather than crashing."""
import csv
import os

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
}


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


def main() -> None:
    build_all(
        kb_dir=KB_DIR,
        course_csv=COURSE_CSV,
        detect_csv=DETECT_CSV,
        telemetry_map=_load_telemetry_map(),
        vd_map=_load_vd_map(),
        out_dir=OUT_DIR,
        capture_map=_load_capture_map(),
    )


if __name__ == "__main__":
    main()
