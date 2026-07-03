# GARAGE — Backend

**GARAGE** (GAme Race Analysis and Guidance Engineer) is a virtual sim race engineering system for
telemetry-based analysis, coaching, and performance guidance across racing games and simulators.

This site documents the **backend**: the software that turns raw game telemetry into structured
data for analysis and coaching. Gran Turismo 7 is the first supported game.

## The system

The backend runs as two cooperating processes:

| Component | Role | Status |
|---|---|---|
| **Telemetry Collector** | Receives GT7 UDP telemetry, decodes it, and streams it onward. | Implemented |
| **Real-Time Processing System (RTPS)** | Session management, lap segmentation, analysis, coaching. | Planned |

They talk over a **versioned, game-neutral telemetry contract**. A collector is game-specific
(this one speaks Gran Turismo 7); the processing system is not, so other games could feed the same
pipeline through their own collectors in the future.

The web dashboard that visualizes results is a separate application and is not part of this backend.

## Pages

- [Getting started](getting-started.md) — install, build, and run the collector.
- [Configuration](configuration.md) — every option, with defaults and precedence.
- [Architecture](architecture.md) — how telemetry flows through the collector.

## Current capabilities

The collector receives Gran Turismo 7 telemetry at ~60 Hz, decrypts and validates each packet,
discards duplicates and stale packets, decodes the full telemetry frame, and streams it to the
processing system over gRPC — reconnecting automatically if that system is unavailable. It logs
structured, levelled diagnostics and shuts down cleanly.

## License

Business Source License 1.1; converts to GPLv3 on 2030-01-01.
