# HTTP API reference

The backend (`rtps`) exposes an HTTP API, mostly read-only (see [Security](#security) for the few
mutating endpoints). It listens on `--api.addr` (default `localhost:8080`). All responses are JSON
unless noted; errors use the shape `{"error": "..."}`. Durations encoded as strings (such as
`"115.583s"`) are protobuf Duration strings; fields ending in `_ns` are integer nanoseconds.

> **Status:** the backend and its API are implemented and run today, but analysis and coaching
> are still evolving — treat the coaching output as work-in-progress (see notes below).

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/sessions` | List detected sessions (summaries). |
| `GET` | `/v1/sessions/{id}/coaching` | Coaching report for one finished session. |
| `GET` | `/v1/live` | Latest frame plus a progressively-enriched snapshot. |
| `GET` | `/v1/live/stream` | Server-Sent Events: one event per fresh frame, plus track events. |
| `POST` | `/v1/live/recording` | Switch recording of the current session on or off. |
| `GET` | `/v1/tracks` | Reference Knowledge Base (RKB) track index. |
| `GET` | `/v1/tracks/{slug}` | Full RKB record for one track. |
| `GET` | `/v1/sessions/{id}/laps` | A session's laps with sector splits and the track each lap was driven on. |
| `GET` | `/v1/tracks/{slug}/reference-lap` | The track's stored reference lap (pinned, else fastest). |
| `PUT` | `/v1/tracks/{slug}/reference-lap` | Pin a stored lap as the track's reference. |
| `DELETE` | `/v1/tracks/{slug}/reference-lap` | Clear the pin (falls back to the fastest lap). |
| `GET` | `/v1/tracks/{slug}/laps` | Stored laps usable as the track's reference, fastest first. |
| `GET` | `/` | The embedded web UI (static SPA; 404 if the frontend was not built). |

### `GET /v1/sessions`

Returns a JSON array of session summaries in store order. Each summary carries: `id`, `car_id`,
`car_name`, `status`, `start_time`, `end_time`, `frame_count`, `gap_count`, `current_lap`,
`total_laps`, `best_lap`.

`best_lap` is the session's best lap time in the same encoding as the live frame's `best_lap` (a
protobuf Duration string such as `"115.583s"`), or `null` when the session has no timed lap.

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

`enrichment.session_id` identifies the session the live frames belong to. When the live session is
an in-game replay of laps already stored (a duplicate, which is not stored again), `session_id` is
the id of the stored original and `enrichment.duplicate_of` carries that same id. `duplicate_of` is
absent when the live session is not a duplicate. Before a duplicate is recognised (at its first
completed lap), `session_id` is the new session's own id.

`enrichment.recording` is always present: `true` by default and at every new session, `false` after
recording was switched off for the current session.

`enrichment.delta_to_best_ns` is the live gap (integer ns; positive = slower) to the session's own
best lap at the same point on track. `enrichment.delta_to_stored_ref_ns` is the live gap to the
current track's stored reference lap (see
[`GET /v1/tracks/{slug}/reference-lap`](#get-v1tracksslugreference-lap)), from any earlier session.
It is `null` while the track is unknown or only provisional, when the track has no stored reference
lap, and on a lap the car did not start at the line (the out lap, or the lap the session joined
mid-way). The reference is loaded when the track locks, and reloaded when its pin changes, when a
lap stored on the track could replace it (the track's first stored lap, or a new fastest lap while
nothing is pinned), and when the session it came from is discarded.

### `POST /v1/live/recording`

Switches recording of the current session. Body: `{"recording": false, "session_id": "<id>"}` or
`{"recording": true}`. `session_id` is optional: the session the client is showing. Switching off then
applies only if that is still the open session (or, while a replay of stored session X plays, X
itself); otherwise nothing changes and the response is `409` `"session changed"`.

- `200` `{"recording": <bool>, "session_id": "<id or empty>"}`: the resulting state. `session_id` is
  the open recorded session (empty after switching off; the new session after switching on).
- `409` (`{"error": "no active session"}`) when no session is open (menus, idle).
- `409` (`{"error": "session changed"}`) when `session_id` no longer names the open session.
- `503` (`{"error": "telemetry stalled; recording unchanged"}`) when no telemetry arrived to apply
  the change within 2 seconds. The request is withdrawn, so it never applies later.
- `400` on a malformed body.

Switching off discards the whole current session: its session, laps and traces are deleted and
nothing more of that stint is stored. Live telemetry (`/v1/live`, the stream) keeps running.
Switching back on while still on track starts a new session from that moment; earlier laps are not
recoverable. Recording is on again at every new session. Setting the current value is a no-op.
The switch applies to the telemetry source running inside `rtps` (the PS5 collector or
`--replay-file`), not to external collectors streaming over gRPC.

> **Security:** see [Security](#security): this endpoint is unauthenticated.

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

### `GET /v1/sessions/{id}/laps`

Returns the session's laps in lap order: `lap_number`, `duration_ns`, `complete`, `valid`, `timed`,
`delta_to_pb_ns`, a `sectors` block, and:

- `track_slug`: the track this lap was driven on, or `null` when the track detector had no guess. A
  session can contain laps on several tracks, so the track belongs to the lap, not the session.
- `track_confidence`: `"provisional"` (the detector's guess when the lap completed; it may still have
  switched) or `"confident"` (the detector locked on this track), or `null`. When a session ends, laps
  the detector never confirmed have their labels cleared to `null`. A finished session can still show
  `"provisional"` only if that clear failed or timed out, or for a stint the detector DID lock whose
  label backfill failed (kept, since the lock confirmed it).
- `reference_eligible`: whether this lap can be its track's stored reference lap: complete, valid,
  `confident` on its track, and not the first lap of a race (`total_laps` > 0). Clients use it to show
  or hide "set as reference" instead of repeating the rule. The field is always present: the web
  dashboard is embedded in the `rtps` binary, so the dashboard and the API always ship together.
  A race's lap 1 becomes ineligible once the session's positive `total_laps` is persisted. That is
  normally before lap 1 completes, because GT7 reports the total from the grid. If it arrives later,
  eligibility updates then, and any pin or live reference on that lap is removed or re-resolved.

A lap completed before the detector locked carries the provisional guess (or `null`) until the lock,
which then marks every lap of that detection run `confident` on the locked track. If the car leaves
the locked track for good (a track change within one session), detection starts again and later laps
get the new track. Each lap's sector splits use its own track's sectors.

- `404` if the session id is unknown.

### `GET /v1/tracks/{slug}/reference-lap`

Returns the stored reference lap for one track layout (`slug` is exact: layout and direction). The
reference is the lap you pinned for the track, or otherwise the fastest stored lap on it that is
complete, valid and was detected `confident` on exactly this slug, across all sessions and cars.
The first lap of a race (lap 1 of a session with `total_laps` > 0) is never eligible: it contains the
start. A time trial's lap 1 is. If the pinned lap stops being eligible (for example a race's lap 1 pinned
before this rule existed), resolving the reference removes the pin, logs "reference pin removed: lap
not eligible" once at INFO, and the response is the fastest eligible lap (`source` `"fastest"`).
`?decimate=N` caps the samples (default 1500; the first and last sample are always kept).

The body has the same fields and shapes as `GET /v1/sessions/{id}/reference`, so the same overlay
code can draw it:

- `reliable`, `reason`: `reliable` is `false` with a machine `reason` (such as `"no-sector-labels"`)
  when the RKB has no sector data for the track; `sectors` then falls back to 3 equal mini-sectors.
- `predicted_best_ns`: the reference lap's time.
- `sectors`: `[{index, label, owner_lap, duration_ns}]`, split on the track's sectors; `owner_lap` is
  the reference lap.
- `samples`: the lap's telemetry on the distance `fraction` axis (`fraction`, `t_ns`, `distance`,
  `pos_x`, `pos_z`, `speed`, `throttle`, `brake`, `rpm`, `gear`, `fuel`, tyre temperatures, `clutch`,
  `boost`, `steering`, `delta_to_best_ns`).

Plus where the lap comes from:

- `source`: `"pinned"` or `"fastest"`.
- `session_id`, `lap_number`: the stored lap.
- `lap_time`: its lap time as a duration string (`"115.583s"`).
- `recorded_at`: when the lap started (RFC 3339).
- `car`: `{"id": <int>, "name": <string or null>, "category": <string or null>}`. `category` is
  GT7's car category code (such as `"GR4"`), sent only by newer telemetry; `null` otherwise.

Responses:

- `200` with the body above.
- `404` (`{"error": "no stored reference for track"}`) when the track has no pinned lap and no
  eligible stored lap.

### `PUT /v1/tracks/{slug}/reference-lap`

Pins a stored lap as the track's reference. Body: `{"session_id": "<id>", "lap_number": <int>}`.
Pinning replaces any earlier pin for the track. A pin disappears when its lap's session is deleted or
discarded; the track then falls back to its fastest lap.

- `200` with the `GET` body (`source` `"pinned"`).
- `400` on a malformed body.
- `404` if the lap does not exist.
- `409` (`{"error": "lap not usable as reference"}`) if the lap is not complete and valid, was not
  driven on this exact `slug`, its track was not detected `confident`, or it is the first lap of a race.

### `DELETE /v1/tracks/{slug}/reference-lap`

Clears the track's pin (no body).

- `200` with the `GET` body for the fallback (`source` `"fastest"`).
- `404` (`{"error": "no stored reference for track"}`) if the track has no eligible stored lap.

### `GET /v1/tracks/{slug}/laps`

Lists the stored laps that can be the track's reference (complete, valid, detected `confident` on
exactly this slug, not a race's first lap), fastest first, across all sessions and cars. `?limit=N` caps the list (default
20). Each row: `session_id`, `lap_number`, `lap_time` (duration string), `recorded_at` (RFC 3339),
`car` (as above), and `pinned` (`true` for the track's pinned lap). `limit` is capped at 200. The
pinned lap is always listed, after the others when it is not among the fastest `limit` laps. An
unknown track or one with no stored laps returns `[]`.

## Security

The API has no authentication. Its mutating endpoints are `POST /v1/live/recording` (switching
recording off discards the current session) and `PUT`/`DELETE /v1/tracks/{slug}/reference-lap`
(changing a track's pinned reference lap). Anyone who can reach `api.addr` can call them.
`api.addr` defaults to `localhost:8080`; only bind it to another interface on a network you trust.

To stop other web pages open in your browser from calling them (cross-site request forgery and DNS
rebinding), every mutating `/v1` request is checked:

- The request's `Host` must be `localhost`, `127.0.0.1` or `::1` (any port), the host of `api.addr`
  (unless that is a wildcard such as `0.0.0.0`), or listed in `api.allowed_hosts` (see
  [configuration](configuration.md)); otherwise `403` (`{"error": "forbidden host"}`). This also
  applies to requests without `Origin`. If you bind `api.addr` to `0.0.0.0` and open the dashboard by
  LAN IP or hostname, add that IP or hostname to `api.allowed_hosts`; it is never inferred.
- If it has an `Origin` header, the origin must be the API's own (the dashboard served by `rtps`) or
  the Vite dev server (`http://localhost:5173`, `http://127.0.0.1:5173`); otherwise `403`
  (`{"error": "forbidden origin"}`). Requests without `Origin` (such as `curl`) are allowed.
- A request with a body (`POST`, `PUT`) must send `Content-Type: application/json` (a `charset`
  parameter is fine); otherwise `415` (`{"error": "content-type must be application/json"}`). With
  `curl`, pass `-H 'Content-Type: application/json'`, since `curl -d` sends a form content type.

`GET` requests (every read endpoint, the live stream and the web UI) are not restricted by any of
these checks.

The API sends no CORS headers.

## The RKB database

The track endpoints read a read-only SQLite database at `$HOME/.garage/rkb.db`. This path is a
fixed convention, not a configuration key. The file is generated offline by the RKB tool — see
[tools/rkb](tools/rkb.md). If it is missing, the backend fails to start.

## Live snapshot fields

The live payload exposes the fields a dashboard needs: speed, gear and suggested gear, RPM, throttle
/ brake / clutch / boost, fuel, tyre temperatures, current lap and position, best and last lap
times, and the in-race / paused flags.
