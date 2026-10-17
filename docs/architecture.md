# Architecture

## The pipeline, end to end

```
                    +--------------------------------- rtps ---------------------------------+
+-----------+  UDP  | +---------------------+   in-process    +--------------------+          |
| GT7 / PS5 | ----> | | Telemetry Collector | --------------> |  Backend (RTPS)    | <-- gRPC  |
+-----------+ <---- | | (or --replay-file)  |  (game-neutral)  +--------------------+  ingest   |
   keep-alive "A"   | +---------------------+                     |            |   (external |
                    |            |                                |            |  collectors)|
                    +------------|--------------------------------|------------|-------------+
                                 v (optional, debug)              v            v HTTP API + SSE
                          raw packet recording              session store   +-----------+
                                                         (memory/postgres)  |    web    |
                                                                            | dashboard |
                                                                            +-----------+
```

Both run in one process, `rtps`. The **collector** is game-specific: it understands Gran Turismo 7's
wire format. The **backend (RTPS)** is not — it consumes a versioned, game-neutral telemetry
contract. Instead of the live UDP socket, the collector can replay a recorded capture
(`--replay-file`) through the identical decode path. External collectors (another game, or the
replay-client tool) still feed the backend over the gRPC ingest server using the same contract;
note that an external stream shares the live view with the in-process source, so prefer
`--replay-file` for replays. The
collector never exposes GT7-specific types across that boundary.

## Inside the collector

Each packet flows through a fixed sequence:

1. **Receive** — read a UDP datagram from the console (~60 Hz) and copy it off the shared buffer.
2. **Record (optional)** — if enabled, tee the raw encrypted bytes to disk for debugging.
3. **Decrypt** — decrypt the packet and verify it is a genuine GT7 packet.
4. **Validate** — confirm the packet is long enough to decode safely.
5. **De-duplicate** — drop duplicates and out-of-order packets, while recognizing a session restart
   as a fresh start rather than stale data.
6. **Decode** — read the full telemetry frame: position, velocity, speed, RPM, gears, tyres, lap
   and race data, fuel, temperatures, and status flags.
7. **Map** — translate the decoded frame into the game-neutral contract, stamping capture metadata
   (receive time, sequence number, a gap marker for lost packets, and the source).
8. **Publish** — hand the frame to the backend's in-process ingest buffer.

A **keep-alive** is sent to the console on a timer; Gran Turismo 7 stops streaming without it.

### Concurrency and resilience

The collector uses a small number of goroutines with clear boundaries:

- The **receive loop** runs the decode sequence inline — it is fast and never performs blocking I/O
  or logging, so it can always keep up with the network.
- The **in-process ingest** consumes frames on its own goroutine. The buffer in front of it is
  bounded and lossy: under back-pressure it drops the oldest frames (keeping the freshest telemetry)
  rather than stalling reception.
- The **raw recorder**, when enabled, writes on its own goroutine behind a bounded buffer, so slow
  disk I/O never blocks reception either.

Shutdown is orderly: on SIGINT/SIGTERM the receiver stops first (no new packets enter), the raw
recording is flushed and closed, then the ingest stream drains and ends (finalizing the session), and
finally the servers stop.

## The telemetry contract

The contract between collectors and the backend is a versioned Protocol Buffers schema
(`gt7.telemetry.v1`). The in-process collector hands frames over directly; an external collector
calls one client-streaming RPC:

```
rpc Ingest(stream TelemetryFrame) returns (Ack);
```

An external collector dials the backend, streams `TelemetryFrame`s, and receives a single `Ack`
(frames received) when the stream closes. Each frame carries the telemetry values plus capture
metadata (receive time, sequence, gap marker, source `LIVE`/`REPLAY`, schema version) so the backend
can segment laps and reason about timing without ever seeing the GT7 wire format.

## Inside the backend (RTPS)

The backend hosts several logical domains behind two network servers:

- a **gRPC ingest server** (`--ingest.addr`, default `localhost:50051`) that accepts external
  collectors' streams (the in-process collector feeds the same ingest logic directly), and
- an **HTTP API server** (`--api.addr`, default `localhost:8080`) that serves reads and a live
  stream — see the [API reference](api.md).

When a frame arrives at the ingest seam it is, in order:

1. **Published for live egress first** (never blocking the ingest goroutine), then
2. fanned out to the processing sinks: session detection, lap segmentation, trace recording, track
   detection, and enrichment.

### Domains

| Domain | Package | Responsibility |
|---|---|---|
| **Session management** | `rtps/session` | Group the frame stream into sessions (one contiguous on-track stint), tracking lifecycle and outcome (active / finished / abandoned). |
| **Processing** | `rtps/processing` | Segment each session into laps and record per-lap telemetry traces — the source of truth for analysis. |
| **Analysis** | `rtps/analysis` | Deterministic (no LLM): Personal-Best selection, lap/sector deltas, consistency, predicted best, and explainable recommendations. |
| **Coaching** | `rtps/coach` | Convert analysis into coaching output. Currently a placeholder passthrough (marked `pending`). |
| **Live egress** | `rtps/live` | Capture every frame at the ingest seam and fan it out to SSE subscribers plus a latest-frame snapshot, independent of the storage path. |
| **Reference / RKB** | `rtps/reference`, `rtps/rkb` | Track identity + sector definitions, and the read-only Track Reference Knowledge Base served by the track API. |
| **API** | `rtps/api` | The read HTTP API and the live SSE stream. |

### Storage

Sessions, laps, and lap traces are persisted through a store interface with two backings:

- **memory** (default) — everything in process; nothing survives a restart.
- **postgres** — a connection pool with embedded migrations run on startup. Lap traces target a
  time-series (hypertable) layout.

The **Reference Knowledge Base** is a separate, read-only SQLite database at `$HOME/.garage/rkb.db`,
generated offline by the [rkb tool](tools/rkb.md). It is opened read-only and is required at
startup.

### Lifecycle

On startup the backend selects storage (migrating Postgres if configured), opens the RKB, wires the
ingest sinks, starts the telemetry source (UDP collector or replay; a failure such as a busy UDP
port fails startup), and runs the gRPC and HTTP servers each on their own goroutine. A replay that
reaches the end of its capture finalizes its session and leaves the servers running. On SIGINT/SIGTERM
the source stops, the in-process stream is finalized, and both servers stop concurrently, each bounded by a short grace period (gRPC drains gracefully then hard-
stops; HTTP shuts down with a deadline).

## Observability

The collector logs through `rtps`'s structured, levelled logger; its receive path only updates counters, which are
logged periodically off the hot path so logging never interferes with reception. The backend logs
JSON at the configured level and supports live reload of the log level when started with a config
file, and the collector's heartbeat interval likewise (every other key requires a restart).
