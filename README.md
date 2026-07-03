# GARAGE — Backend

**GARAGE** (GAme Race Analysis and Guidance Engineer) is a virtual sim race engineering system for
telemetry-based analysis, coaching, and performance guidance across racing games and simulators.

This repository holds the **backend**: the parts that run behind the scenes, turning raw game
telemetry into structured data for analysis and coaching. Gran Turismo 7 is the first supported
game; the web dashboard lives in a separate repository.

The backend is split into two processes:

- **Telemetry Collector** — receives GT7's UDP telemetry, decodes it, and streams it to the
  processing system over gRPC. **(Implemented.)**
- **Real-Time Processing System (RTPS)** — session management, lap segmentation, analysis, and
  coaching. **(Planned.)**

They communicate over a versioned, game-neutral telemetry contract, so any game-specific collector
that speaks the contract can feed the same processing system.

## Status

The **Telemetry Collector MVP is complete**: it receives, decrypts, de-duplicates, decodes, and
publishes telemetry end to end, with structured logging and graceful shutdown. It has been verified
against real captured telemetry and a loopback integration test. Validation against live PlayStation
hardware, and the RTPS itself, are next.

## What the collector does

- Receives Gran Turismo 7 UDP telemetry (~60 Hz).
- Decrypts and validates each packet.
- De-duplicates packets, tolerant of session restarts and out-of-order delivery.
- Decodes the full telemetry frame (position, speed, RPM, gear, tyres, lap data, and more).
- Maps it to a versioned, game-neutral schema and streams it to the RTPS over gRPC, reconnecting
  automatically if the RTPS is unavailable.
- Sends the keep-alive the game requires to keep telemetry flowing.
- Optionally records raw packets to disk for debugging.
- Emits structured, levelled logs and shuts down cleanly on SIGINT/SIGTERM.

## Quick start

Requires **Go 1.26+**.

```bash
# build
go build -o collector ./cmd/collector

# run (point it at your console and the processing system)
./collector --ps5.ip 192.168.1.50 --rtps.addr localhost:50051
```

The console IP is required. Everything else has sensible defaults. See
[configuration](docs/configuration.md) for all options.

## Documentation

- [Overview](docs/index.md)
- [Getting started](docs/getting-started.md)
- [Configuration reference](docs/configuration.md)
- [Architecture](docs/architecture.md)

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
