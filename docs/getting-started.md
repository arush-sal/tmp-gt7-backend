# Getting started

## Prerequisites

- **Go 1.26 or newer.**
- A Gran Turismo 7 console on the same network (for live use). You can build, test, and run
  everything without one — replay a recorded capture with `--replay-file` instead.
- For the track API: the Reference Knowledge Base database at `$HOME/.garage/rkb.db` (see
  [tools/rkb](tools/rkb.md)). The backend requires it at startup.

## Build

```bash
go build -o rtps ./cmd/rtps   # the one binary: GT7 collector + backend + dashboard
```

Or build everything into `./bin/`:

```bash
make build
```

## Run it

`rtps` runs the GT7 collector in-process, plus the HTTP API, the dashboard, and a gRPC ingest server
(for external collectors and the replay-client; prefer `--replay-file` for replays, since external streams share the live view with the in-process source). Exactly one telemetry source is required:

```bash
# live: the collector sends a keep-alive to the console, and Gran Turismo 7 streams telemetry back
./rtps --ps5.ip 192.168.1.50 --storage memory

# replay: play a recorded capture at its original cadence (x --replay.speed), then keep serving
./rtps --replay-file capture.pcapng --replay.speed 4 --storage memory
```

Passing neither or both of `--ps5.ip` / `--replay-file` is a configuration error. A replayed session
is indistinguishable from a live one, so replay into a dedicated (e.g. `--storage memory`) instance.
Other defaults are sensible — see the [configuration reference](configuration.md).

## Everything at once

`make deploy-stack` is the one command for a race session. It starts TimescaleDB (compose service
`timescaledb`, waiting until healthy), builds the web dashboard and `rtps`, and runs `rtps` in the
background, logging to `bin/rtps.log`:

```bash
make deploy-stack IP=192.168.1.99
# options: STORAGE=memory (no database)  DB_DSN='postgres://...'  API_ADDR=host:port
make replay-stack FILE=capture.pcapng SPEED=4 STORAGE=memory   # same, replaying a capture
make stop-stack   # stop rtps (and timescaledb; its data volume is kept)
```

The dashboard is served by the backend at `http://localhost:8080/`.

Bind the API wide (`API_ADDR=0.0.0.0:8080`) to open the dashboard from another device.

## Verify it's working

Set the log level to `debug` to see receive activity:

```bash
./rtps --ps5.ip 192.168.1.50 --log.level debug
```

Every few seconds it logs a `telemetry stats` line with counts of published frames and any dropped,
foreign, or gapped packets — a quick health check.

Hit the API directly:

```bash
curl http://localhost:8080/v1/live       # latest frame + snapshot
curl http://localhost:8080/v1/sessions   # detected sessions
curl http://localhost:8080/v1/tracks     # RKB track index
```

## Recording raw telemetry (debugging)

To capture raw packets to disk for later inspection (and later decoding with
[pcap2csv](tools/pcap2csv.md)):

```bash
./rtps --ps5.ip 192.168.1.50 --rawdump.enabled --rawdump.dir ./captures
```

This is a debugging aid and is off by default.

## Development tasks

```bash
make hooks   # enable the pre-commit checks (once per clone)
make test    # run the test suite
make all     # format check, lint, and test
make vuln    # scan dependencies for known vulnerabilities
```

Tests run without a console — the decoder is exercised against captured telemetry fixtures, and the
full pipeline against loopback UDP and synthesized captures.
