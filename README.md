# GARAGE — Backend

**GARAGE** (GAme Race Analysis and Guidance Engineer) is a virtual sim race engineering system for
telemetry-based analysis, coaching, and performance guidance across racing games and simulators.

This repository holds the **backend**: the parts that run behind the scenes, turning raw game
telemetry into structured data for analysis and coaching. Gran Turismo 7 is the first supported
game; the web dashboard lives in `web/`.

The backend is made of two cooperating processes plus a terminal client:

- **Telemetry Collector** (`cmd/collector`) — receives GT7's UDP telemetry, decodes it, and streams
  it to the backend over gRPC.
- **Backend / RTPS** (`cmd/rtps`) — session detection, lap segmentation, analysis, live egress, and
  a read HTTP API. Consumes the collector's stream.
- **Terminal dashboard** (`cmd/tui`) — a text-mode pit-wall client of the backend's API.

The collector and backend communicate over a versioned, game-neutral telemetry contract, so any
game-specific collector that speaks the contract can feed the same backend.

## Status

The **end-to-end path works today**: collect -> ingest -> detect sessions -> segment laps ->
analyze -> serve over HTTP/SSE. The collector MVP is complete and has been verified against real
captured telemetry and live PlayStation hardware. The backend's **analysis** layer is deterministic
and functional; the **coaching** layer is currently a placeholder passthrough — treat
natural-language coaching as work-in-progress.

## What each part does

**Collector**

- Receives Gran Turismo 7 UDP telemetry (~60 Hz), decrypts and validates each packet.
- De-duplicates packets, tolerant of session restarts and out-of-order delivery.
- Decodes the full telemetry frame (position, speed, RPM, gear, tyres, lap data, and more).
- Maps it to a versioned, game-neutral schema and streams it to the backend over gRPC, reconnecting
  automatically if the backend is unavailable.
- Sends the keep-alive the game requires, optionally records raw packets, and emits structured logs.

**Backend (RTPS)**

- Accepts the collector's stream on a gRPC ingest server.
- Detects sessions, segments laps, records per-lap traces, detects the track, and analyzes for
  Personal Best, deltas, consistency, and recommendations.
- Serves a read HTTP API and a live Server-Sent-Events stream.
- Stores sessions/laps in memory or Postgres; serves an offline-built Track Reference Knowledge Base
  (RKB) from SQLite.

## Quick start

Requires **Go 1.26+**.

```bash
# build all three binaries into ./bin/
make build

# run the backend (gRPC ingest + HTTP API)
./bin/rtps --api.addr localhost:8080 --ingest.addr localhost:50051

# run the collector (point it at your console and the backend)
./bin/collector --ps5.ip 192.168.1.50 --rtps.addr localhost:50051

# watch the live pit wall
./bin/tui --api http://localhost:8080
```

Or run all three together against a console:

```bash
make live IP=192.168.1.99
```

The console IP is required for live capture. Everything else has sensible defaults. See
[configuration](docs/configuration.md) for all options.

> The backend's track API requires the RKB database at `$HOME/.garage/rkb.db`, built by the
> [rkb tool](docs/tools/rkb.md).

## Web UI

The React dashboard in `web/` is embedded into `rtps` and served at http://localhost:8080/ (the API
stays under `/v1/`). Build it with `make web-build` before `go build`; for development run
`make web-up` (Vite dev server on :5173, proxying `/v1` to the backend).

## Tools

Under `tools/rkb/`:

- **`pcap2csv`** — decode a captured `.pcapng` GT7 stream to a full-field CSV.
- **`vd2csv`** — decode a VictoryDash replay export to a full-field CSV.
- **`rkb`** — offline Python pipeline that builds the Track RKB (`rkb.db`, index, per-track JSON).

See [docs/tools](docs/tools/index.md).

## Documentation

- [Overview](docs/index.md)
- [Getting started](docs/getting-started.md)
- [Configuration reference](docs/configuration.md)
- [Architecture](docs/architecture.md)
- [HTTP API reference](docs/api.md)
- [Terminal dashboard](docs/tui.md)
- [Tools](docs/tools/index.md)

## Development

```bash
make hooks   # once per clone: enable the pre-commit gate
make test    # run tests
make all     # format check, lint, and test
make vuln    # scan dependencies for known vulnerabilities
```

Tests run without a console — the decoder is exercised against captured telemetry fixtures, and the
full pipeline against an in-process gRPC server and loopback UDP.

## License

Business Source License 1.1 (BSL). See [`LICENSE`](LICENSE). The license converts to GPLv3 on
2030-01-01.
