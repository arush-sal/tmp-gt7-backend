# Architecture

## Two processes, one contract

```
+-----------+   UDP telemetry    +---------------------+   gRPC stream    +--------------------+
| GT7 / PS5 | -----------------> | Telemetry Collector | ---------------> | Processing System  |
+-----------+   keep-alive "A"   +---------------------+  (game-neutral)  |      (RTPS)         |
                                           |                              +--------------------+
                                           v (optional, debug)
                                    raw packet recording
```

The **collector** is game-specific: it understands Gran Turismo 7's wire format. The **processing
system (RTPS)** is not — it consumes a versioned, game-neutral telemetry contract. Splitting them
means a different game's collector could feed the same processing system by speaking the same
contract. The collector never exposes GT7-specific types across that boundary.

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
8. **Publish** — hand the frame to the streaming publisher.

A **keep-alive** is sent to the console on a timer; Gran Turismo 7 stops streaming without it.

## Concurrency and resilience

The collector uses a small number of goroutines with clear boundaries:

- The **receive loop** runs the decode sequence inline — it is fast and never performs blocking I/O
  or logging, so it can always keep up with the network.
- The **publisher** owns the gRPC connection on its own goroutine. It reconnects with backoff if the
  processing system is unavailable, and its outbound buffer is bounded and lossy: under back-pressure
  it drops the oldest frames (keeping the freshest telemetry) rather than stalling reception.
- The **raw recorder**, when enabled, writes on its own goroutine behind a bounded buffer, so slow
  disk I/O never blocks reception either.

Shutdown is orderly: on SIGINT/SIGTERM the receiver stops first (no new packets enter), then the
publisher winds down and the raw recording is flushed and closed.

## Observability

The collector emits structured, levelled logs. The receive path itself only updates counters;
those counts — published frames, and any dropped, foreign, short, or gapped packets — are logged
periodically off the hot path, so logging never interferes with reception.

## The telemetry contract

The contract between the collector and the processing system is defined as a Protocol Buffers
schema and is versioned, so it can evolve without breaking either side. It carries the telemetry
values plus the capture metadata the processing system needs to segment laps and reason about
timing.
