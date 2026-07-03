# Getting started

## Prerequisites

- **Go 1.26 or newer.**
- A Gran Turismo 7 console on the same network (for live use). You can build, test, and run the
  collector without one — it just won't receive telemetry until a console is streaming.

## Build

```bash
go build -o collector ./cmd/collector
```

## Run

The collector needs your console's IP address and, for real use, the address of the processing
system it streams to:

```bash
./collector --ps5.ip 192.168.1.50 --rtps.addr localhost:50051
```

- `--ps5.ip` is **required** — the collector sends a keep-alive to the console at this address, and
  Gran Turismo 7 streams telemetry back.
- `--rtps.addr` defaults to `localhost:50051`. The collector dials it and streams decoded frames; if
  it is unreachable, the collector keeps trying and reconnects automatically.

All other options have defaults — see the [configuration reference](configuration.md).

## Verify it's working

Set the log level to `debug` to see connection and receive activity, and watch the periodic
telemetry statistics:

```bash
./collector --ps5.ip 192.168.1.50 --log.level debug
```

Every few seconds the collector logs a `telemetry stats` line with counts of published frames and
any dropped, foreign, or gapped packets — a quick health check.

## Recording raw telemetry (debugging)

To capture raw packets to disk for later inspection:

```bash
./collector --ps5.ip 192.168.1.50 --rawdump.enabled --rawdump.dir ./captures
```

This is a debugging aid and is off by default.

## Development tasks

```bash
make hooks   # enable the pre-commit checks (once per clone)
make test    # run the test suite
make all     # format check, lint, and test
```
