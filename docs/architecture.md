# Architecture

## The pipeline, end to end

```
+-----------+   UDP telemetry    +---------------------+   gRPC stream    +--------------------+
| GT7 / PS5 | -----------------> | Telemetry Collector | ---------------> |  Backend (RTPS)    |
+-----------+   keep-alive "A"   +---------------------+  (game-neutral)  +--------------------+
                                           |                                 |   |
                                           v (optional, debug)               |   v HTTP API + SSE
                                    raw packet recording                     |  +-----------+
                                                                             |  | tui /     |
                                                                             |  | dashboard |
                                                                             v  +-----------+
                                                                        session store
                                                                       (memory / postgres)
```

The **collector** is game-specific: it understands Gran Turismo 7's wire format. The **backend
(RTPS)** is not — it consumes a versioned, game-neutral telemetry contract. Splitting them means a
different game's collector could feed the same backend by speaking the same contract. The collector
never exposes GT7-specific types across that boundary.

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
8. **Publish** — hand the frame to the streaming publisher, which streams it to the backend.

A **keep-alive** is sent to the console on a timer; Gran Turismo 7 stops streaming without it.

### Concurrency and resilience

The collector uses a small number of goroutines with clear boundaries:

- The **receive loop** runs the decode sequence inline — it is fast and never performs blocking I/O
  or logging, so it can always keep up with the network.
- The **publisher** owns the gRPC connection on its own goroutine. It reconnects with backoff if the
  backend is unavailable, and its outbound buffer is bounded and lossy: under back-pressure it drops
  the oldest frames (keeping the freshest telemetry) rather than stalling reception.
- The **raw recorder**, when enabled, writes on its own goroutine behind a bounded buffer, so slow
  disk I/O never blocks reception either.

Shutdown is orderly: on SIGINT/SIGTERM the receiver stops first (no new packets enter), then the
publisher winds down and the raw recording is flushed and closed.

## The telemetry contract

The contract between the collector and the backend is a versioned Protocol Buffers schema
(`gt7.telemetry.v1`). The collector calls one client-streaming RPC:

```
rpc Ingest(stream TelemetryFrame) returns (Ack);
```

The collector dials the backend, streams `TelemetryFrame`s, and receives a single `Ack`
(frames received) when the stream closes. Each frame carries the telemetry values plus capture
metadata (receive time, sequence, gap marker, source `LIVE`/`REPLAY`, schema version) so the backend
can segment laps and reason about timing without ever seeing the GT7 wire format.

## Inside the backend (RTPS)

The backend hosts several logical domains behind two network servers:

- a **gRPC ingest server** (`--ingest.addr`, default `localhost:50051`) that accepts the collector's
  stream, and
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
ingest sinks, and runs the gRPC and HTTP servers each on their own goroutine. On SIGINT/SIGTERM both
servers stop concurrently, each bounded by a short grace period (gRPC drains gracefully then hard-
stops; HTTP shuts down with a deadline).

## Observability

The collector emits structured, levelled logs; its receive path only updates counters, which are
logged periodically off the hot path so logging never interferes with reception. The backend logs
JSON at the configured level and supports live reload of the log level when started with a config
file (every other key requires a restart).
