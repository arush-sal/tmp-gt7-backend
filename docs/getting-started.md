# Getting started

## Prerequisites

- **Go 1.26 or newer.**
- A Gran Turismo 7 console on the same network (for live use). You can build, test, and run
  everything without one — the collector just won't receive telemetry until a console is streaming.
- For the track API: the Reference Knowledge Base database at `$HOME/.garage/rkb.db` (see
  [tools/rkb](tools/rkb.md)). The backend requires it at startup.

## Build

```bash
go build -o collector ./cmd/collector   # GT7 telemetry collector
go build -o rtps      ./cmd/rtps         # backend (ingest + HTTP API)
```

Or build everything into `./bin/`:

```bash
make build
```

## Run the pieces

### Backend

```bash
./rtps --api.addr localhost:8080 --ingest.addr localhost:50051 --storage memory
```

It starts a gRPC ingest server (for the collector) and an HTTP API (for clients). Defaults are
sensible — see the [configuration reference](configuration.md).

### Collector

```bash
./collector --ps5.ip 192.168.1.50 --rtps.addr localhost:50051
```

- `--ps5.ip` is **required** — the collector sends a keep-alive to the console at this address, and
  Gran Turismo 7 streams telemetry back.
- `--rtps.addr` defaults to `localhost:50051` (the backend's ingest address). If it is unreachable,
  the collector keeps trying and reconnects automatically.

## Everything at once

`make deploy-stack` is the one command for a race session. It starts TimescaleDB (compose service
`timescaledb`, waiting until healthy), builds the web dashboard and the binaries, and runs the
backend and collector in the background, logging to `bin/rtps.log` and `bin/collector.log`:

```bash
make deploy-stack IP=192.168.1.99
# options: STORAGE=memory (no database)  DB_DSN='postgres://...'  API_ADDR=host:port
make stop-stack   # stop them (and timescaledb; its data volume is kept)
```

The dashboard is served by the backend at `http://localhost:8080/`.

Bind the API wide (`API_ADDR=0.0.0.0:8080`) to open the dashboard from another device.

## Verify it's working

Set the collector log level to `debug` to see connection and receive activity:

```bash
./collector --ps5.ip 192.168.1.50 --log.level debug
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
./collector --ps5.ip 192.168.1.50 --rawdump.enabled --rawdump.dir ./captures
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
full pipeline against an in-process gRPC server and loopback UDP.
