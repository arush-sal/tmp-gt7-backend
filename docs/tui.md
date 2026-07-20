# Terminal dashboard (tui)

`tui` is a terminal dashboard for the backend HTTP API — a text-mode "pit wall". It is interactive
by default and can also render a one-shot snapshot to stdout.

It contains no racing logic: it is a pure client of the [HTTP API](api.md).

## Build and run

```bash
go build -o tui ./cmd/tui
./tui --api http://localhost:8080
```

## Options

| Flag | Default | Description |
|---|---|---|
| `--api` | `http://localhost:8080` | Backend HTTP API base URL. |
| `--once` | `false` | Render a snapshot to stdout and exit (no interactive UI). |
| `--session` | *(none)* | With `--once`, also render this session's coaching report. |

## Interactive mode

The default mode is an interactive app with three views:

- **Live** (default) — a streaming dashboard fed by the `/v1/live/stream` SSE endpoint: speed, gear
  and suggested gear, RPM, pedals, fuel, tyre temperatures, lap and position, best and last lap, and
  the in-race / paused flags. It reconnects automatically if the stream drops.
- **Sessions** — the list of detected sessions (`/v1/sessions`).
- **Detail** — a per-session coaching report (`/v1/sessions/{id}/coaching`).

## Snapshot mode

```bash
# print the session list and exit
./tui --once

# also print one session's report
./tui --once --session <session-id>
```

Snapshot mode is handy for scripting or for a quick check without a full-screen UI.
