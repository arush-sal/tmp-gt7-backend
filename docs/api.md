# HTTP API reference

The backend (`rtps`) exposes a read-only HTTP API. It listens on `--api.addr` (default
`localhost:8080`). All responses are JSON unless noted; errors use the shape
`{"error": "..."}`.

> **Status:** the backend and its API are implemented and run today, but analysis and coaching
> are still evolving — treat the coaching output as work-in-progress (see notes below).

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/sessions` | List detected sessions (summaries). |
| `GET` | `/v1/sessions/{id}/coaching` | Coaching report for one finished session. |
| `GET` | `/v1/live` | Latest frame plus a progressively-enriched snapshot. |
| `GET` | `/v1/live/stream` | Server-Sent Events: one event per fresh frame, plus track events. |
| `GET` | `/v1/tracks` | Reference Knowledge Base (RKB) track index. |
| `GET` | `/v1/tracks/{slug}` | Full RKB record for one track. |
| `GET` | `/` | The embedded web UI (static SPA; 404 if the frontend was not built). |

### `GET /v1/sessions`

Returns a JSON array of session summaries in store order. Each summary carries: `id`, `car_id`,
`car_name`, `status`, `start_time`, `end_time`, `frame_count`, `gap_count`, `current_lap`,
`total_laps`.

`status` is one of `active`, `finished`, or `abandoned`.

### `GET /v1/sessions/{id}/coaching`

Returns the coaching report for one session: the deterministic analysis (Personal Best, per-lap and
per-sector deltas, consistency, predicted best, recommendations) wrapped with a coaching layer.

- `404` if the session id is unknown.
- `409` if the session is still active — use `/v1/live` for a session in progress.
- `500` if the report cannot be produced.

> The `coaching` block is currently a placeholder passthrough (`{"status": "pending", ...}`). The
> analysis values it wraps are real; the natural-language coaching layer is not built yet.

### `GET /v1/live`

Returns the current frame plus a live snapshot: `served_at`, `status`, the canonical `frame`
(protojson), a derived block, and progressively-resolved enrichment (analysis, detected track, and
more as they become available).

### `GET /v1/live/stream`

A [Server-Sent Events](https://developer.mozilla.org/docs/Web/API/Server-sent_events) stream
(`Content-Type: text/event-stream`). It emits:

- one default `data:` event per fresh frame (the same payload as `/v1/live`);
- an `event: track` event when the detected track changes, carrying `{slug, confidence, record}`
  (pushed once per track change, not per frame);
- a `: ping` comment every 15 seconds to keep the connection alive.

### `GET /v1/tracks`

Returns the RKB index — the track picker rows (slug, name, tier, confidence, and so on) used to
browse available reference tracks.

- `503` if the RKB database is unavailable.

### `GET /v1/tracks/{slug}`

Returns the full assembled RKB record for one track: world-metre geometry (affine transform,
boundary), corners, sectors, track width, a confidence gate, and provenance.

- `404` if the slug is unknown.
- `503` if the RKB database is unavailable.

## The RKB database

The track endpoints read a read-only SQLite database at `$HOME/.garage/rkb.db`. This path is a
fixed convention, not a configuration key. The file is generated offline by the RKB tool — see
[tools/rkb](tools/rkb.md). If it is missing, the backend fails to start.

## Live snapshot fields

The live payload exposes the fields a dashboard needs: speed, gear and suggested gear, RPM, throttle
/ brake / clutch / boost, fuel, tyre temperatures, current lap and position, best and last lap
times, and the in-race / paused flags.
