# GARAGE — Backend

**GARAGE** (GAme Race Analysis and Guidance Engineer) is a virtual sim race engineering system for
telemetry-based analysis, coaching, and performance guidance across racing games and simulators.

This site documents the **backend**: the software that turns raw game telemetry into structured
data for analysis and coaching. Gran Turismo 7 is the first supported game. The web dashboard that
visualizes results is built into the backend and served at `/`.

## The system

The backend is one binary, `rtps`, whose parts talk over a **versioned, game-neutral telemetry
contract**. The collector is game-specific (this one speaks Gran Turismo 7) and runs in-process; the
processing system is not, so other games could feed the same pipeline through their own collectors
over the gRPC ingest server.

| Component | Role | Status |
|---|---|---|
| **Telemetry Collector** (in `rtps`) | Receives GT7 UDP telemetry (or replays a capture), decodes it, hands it to ingest in-process. | Implemented |
| **Real-Time Processing System** (`rtps`) | Session detection, lap segmentation, analysis, live egress, and a read HTTP API. | Implemented (analysis/coaching evolving) |
| **Web dashboard** (`web/`) | React UI embedded into `rtps` and served at `/`. | Implemented |

> **Status note.** The end-to-end path — collect -> ingest -> detect sessions -> segment laps ->
> analyze -> serve — works today. The **analysis** layer is deterministic and functional; the
> **coaching** layer is currently a placeholder passthrough, so treat natural-language coaching as
> work-in-progress.

## Binaries

- `cmd/rtps` — the whole platform: GT7 collector (or capture replay), gRPC ingest, HTTP API, dashboard.

## Pages

- [Getting started](getting-started.md) — install, build, and run `rtps`.
- [Configuration](configuration.md) — every option, with defaults and precedence.
- [Architecture](architecture.md) — how telemetry flows end to end.
- [HTTP API reference](api.md) — the backend's read endpoints and live stream.
- [Tools](tools/index.md) — the `pcap2csv`, `vd2csv`, and `rkb` data tools.

## License

Business Source License 1.1; converts to GPLv3 on 2030-01-01.
