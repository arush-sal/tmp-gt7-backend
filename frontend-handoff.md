# GARAGE Backend — Frontend API Handoff

Contract for the web dashboard (separate repo). The backend exposes a small **read-only HTTP/JSON
API** over the deterministic analysis pipeline. Telemetry ingestion is a separate gRPC path the
collector uses; the frontend only touches the HTTP API below.

## Base URL & versioning

- Default: `http://localhost:8080` (configurable via `--api.addr` / `GT7_API_ADDR`).
- All routes are under `/v1`. Responses are `application/json`.
- Read-only today: only `GET`. No auth in this version (single-user/local).

## Live telemetry (primary)

- `GET /v1/live` (and `/` alias) — snapshot: `{served_at, status, frame, derived, enrichment}`.
  `frame` carries ALL canonical fields (snake_case); `derived` has `current_lap_time_ns`, `fuel_pct`,
  `source`; `enrichment` holds `session_id` (present once a session starts) plus correlated fields that
  fill in as they resolve (`analysis` per lap; `car_name`/`track`/`delta_to_best_ns` reserved, null for
  now).
- `GET /v1/live/stream` — Server-Sent Events, one `data:` JSON payload (the same snapshot shape) per
  frame at line rate. A fresh client also gets the current snapshot immediately on connect. Slow clients
  coalesce to the freshest frame. `: ping` heartbeat every 15s.
- Idle (no telemetry yet): `200` with `status:"idle"` and `frame:null`; the other top-level keys are
  still present (`served_at`, `derived:null`, `enrichment` as usual) — e.g.
  `{"served_at":"…","status":"idle","frame":null,"derived":null,"enrichment":{…}}`.

The `/v1/sessions*` endpoints below are the **retention / post-race analysis** surface, not the live
surface — a session summary is intentionally not live-updated mid-race.

## Conventions

- **Durations are integer NANOSECONDS.** Every duration field ends in `_ns`. Divide by `1e6` for
  milliseconds, `1e9` for seconds. (e.g. `pb_time_ns: 9657639900` = 9.658 s.)
- **Timestamps** are RFC 3339 / ISO 8601 UTC strings (`start_time`, `end_time`).
- `car_id` is GT7's numeric model id (human name resolution is a future RKB feature — see Caveats).
- Field names are `snake_case`.

## Endpoints

### `GET /v1/sessions`
List detected sessions (one on-track stint each), in store order.

Response `200`: array of **SessionSummary**

| field | type | notes |
|---|---|---|
| `id` | string | session id |
| `car_id` | int | GT7 car model id |
| `status` | string | `active` \| `finished` \| `abandoned` (abandoned = quit before the lap target) |
| `start_time` | string (RFC3339) | first in-race frame |
| `end_time` | string (RFC3339) | last in-race frame (zero value for an active session) |
| `frame_count` | int | in-race frames |
| `gap_count` | int | frames flagged as following a dropped packet |
| `current_lap` | int | last in-race lap counter |
| `total_laps` | int | event lap target (may be `-1`/`0` for some modes) |

```json
[
  {"id":"sess-1","car_id":1384,"status":"finished","start_time":"2026-07-03T14:18:54.168Z",
   "end_time":"2026-07-03T14:22:03.473Z","frame_count":11283,"gap_count":0,"current_lap":3,"total_laps":2},
  {"id":"sess-5","car_id":2108,"status":"abandoned","start_time":"2026-07-03T14:25:07.777Z",
   "end_time":"2026-07-03T14:27:35.774Z","frame_count":8825,"gap_count":0,"current_lap":1,"total_laps":10}
]
```

### `GET /v1/sessions/{id}/coaching`
The coaching report for one session: deterministic analysis + a coaching layer. **Finished/abandoned
sessions only** — an active session's stored best-lap is frozen at confirm-time, so this endpoint
returns `409` while the session is still in progress; poll `/v1/live` instead for the live view.

Response `200`: **Report**
- `analysis` — **SessionAnalysis**
- `coaching` — **Coaching** `{ status: string, note: string }` — currently `status:"pending"` (the
  coaching layer is a passthrough placeholder; see Caveats).

**SessionAnalysis**

| field | type | notes |
|---|---|---|
| `session_id` | string | |
| `has_pb` | bool | false when no valid lap (e.g. abandoned session) — then the rest is empty |
| `pb_lap` | int | Personal-Best lap number; `0` when the GT7-authoritative best doesn't match any captured lap |
| `pb_time_ns` | int (ns) | PB lap time (GT7's own authoritative best, not derived from captured laps) |
| `laps` | LapAnalysis[] | one per valid lap |
| `consistency` | Consistency | spread of valid laps |
| `theoretical_best_ns` | int (ns) | Σ of the fastest sector times achieved (an optimal lap; ≤ PB) |
| `time_on_table_ns` | int (ns) | PB sector-sum − theoretical best (achievable gain the driver has shown piece-by-piece) |
| `best_sectors` | SectorBest[] | the fastest sector times + which lap set each |
| `recommendations` | string[] | human-readable, data-supported statements |

**LapAnalysis**: `lap_number` int, `duration_ns` int, `delta_to_pb_ns` int (0 for the PB lap),
`sector_deltas` SectorDelta[].
**SectorDelta**: `index` int, `lap_time_ns` int, `pb_time_ns` int, `delta_to_pb_ns` int (positive =
slower than PB in that sector).
**Consistency**: `count` int, `best_ns` int, `mean_ns` int, `std_dev_ns` int, `spread_ns` int.
**SectorBest**: `index` int, `duration_ns` int, `lap_number` int.

```json
{
  "analysis": {
    "session_id": "sess-6", "has_pb": true, "pb_lap": 1, "pb_time_ns": 9657639900,
    "laps": [
      {"lap_number":1,"duration_ns":9657639900,"delta_to_pb_ns":0,
       "sector_deltas":[{"index":0,"lap_time_ns":9657639900,"pb_time_ns":9657639900,"delta_to_pb_ns":0}]}
    ],
    "consistency": {"count":1,"best_ns":9657639900,"mean_ns":9657639900,"std_dev_ns":0,"spread_ns":0},
    "theoretical_best_ns": 9657639900, "time_on_table_ns": 0,
    "best_sectors": [{"index":0,"duration_ns":9657639900,"lap_number":1}],
    "recommendations": ["Personal Best: lap 1 at 0:09.658."]
  },
  "coaching": {"status":"pending","note":"Coaching layer not yet implemented; ..."}
}
```

## Errors

Non-2xx responses use a stable shape: `{ "error": "<message>" }`.

| status | when |
|---|---|
| `404` | unknown session id (`GET /v1/sessions/{id}/coaching`) |
| `409` | session still active (`GET /v1/sessions/{id}/coaching`) — use `/v1/live` instead |
| `500` | analysis failure |

```json
{"error":"session not found"}
```

## Caveats / not-yet (so the UI can degrade gracefully)

- **Coaching is a placeholder** (`status:"pending"`). Real, corner-level, improvement-oriented coaching
  (mistakes/priorities/drills) is deferred until there's enough data; for now the UI should surface
  the `analysis` (PB, deltas, theoretical best, consistency, recommendations) and show coaching as
  pending.
- **Sectors** are currently equal-distance mini-sectors (the RKB that supplies real track/named sectors
  is not wired yet). Sector indices are `0..N-1`, not named turns.
- **Car names / track names** are not resolved yet (`car_id` only; RKB + car dataset pending). Show the
  id with a fallback label.
- **PB scope is per-session** (cross-session/all-time PB needs track identity from the RKB).
- Field additions are backward-compatible; treat unknown fields leniently.

## Running locally

```bash
# everything at once against a PS5 (backend + collector + TUI; Ctrl-C stops all):
make live IP=192.168.1.99

# or piece by piece:
go run ./cmd/rtps --storage=memory            # gRPC ingest :50051, HTTP API :8080
# stream telemetry (collector, or the replay-client tool), then:
curl localhost:8080/v1/sessions
curl localhost:8080/v1/sessions/<id>/coaching
# or the terminal dashboard:
go run ./cmd/tui --api http://localhost:8080          # interactive
go run ./cmd/tui --api http://localhost:8080 --once   # snapshot to stdout
```
