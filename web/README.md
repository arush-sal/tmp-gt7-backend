# GT7 Race Coach - Frontend

Web dashboard for the GT7 telemetry coaching system. It renders a live view of a Gran
Turismo 7 session - position, lap, delta-to-best, lap history, tyre temps, and fuel -
by consuming the RTPS backend's live API.

- **Stack:** React 18, TypeScript, Vite, Vitest.
- **Data source:** the RTPS backend's Server-Sent Events stream `GET /v1/live/stream`
  (single source of truth). The frontend validates every frame at the boundary and maps
  it to display values; it never invents data.

## Prerequisites

- Node.js 18+ and npm.
- The RTPS backend for live data (optional). Without it the dashboard shows a
  "connecting - waiting for telemetry" banner. See the API contract in
  `frontend-handoff.md`.

## Quickstart

```bash
make web-install      # install dependencies (run from the backend root)
make web-up           # dev server on http://localhost:5173, proxying /v1 -> localhost:8080
```

Open http://localhost:5173. On first run you'll go through a short onboarding; after that
it lands on the dashboard.

### Point at a specific backend

`make web-up` proxies the live API to `localhost:8080` by default. To target a different RTPS
address (e.g. a machine running the backend on your LAN):

```bash
make web-up RTPS=192.168.1.50:8080     # host:port, or a full http(s):// origin
```

`RTPS` is a make variable (not a positional argument) because a bare `host:port` collides
with Make's `:` rule separator. It flows into the Vite dev proxy via `GT7_API_ADDR`
(see `vite.config.ts`). In production, serve the app same-origin as the API or route
`/v1/*` to it at the edge.

## Commands

Run these from the backend root (the parent of `web/`).

| Command | What |
|---|---|
| `make web-up [RTPS=host:port]` | Start the dev server (Vite) with the live proxy |
| `make web-build` | Production build (`tsc && vite build`) into `web/dist`, embedded by rtps |
| `make web-preview` | Serve the production build locally |
| `make web-install` | Install dependencies (`npm ci`) |
| `make web-test` | Run the tests |

Underlying npm scripts: `npm run dev`, `npm run build`, `npm run preview`, `npm test`
(Vitest, one shot), `npm run test:watch`.

## Testing

```bash
npm test        # run the unit + hook + integration suites once
```

Pure logic (validation, formatting, view-model) runs in Node; the React hook and the
Dashboard integration test run under jsdom (via a per-file `// @vitest-environment jsdom`
docblock).

## Layout

```
src/
  contract/live.ts     # LiveSnapshot - the /v1/live wire type
  lib/
    validateLive.ts    # runtime guard (trust boundary)
    liveSource.ts      # injectable EventSource subscription
    liveView.ts        # snapshot -> display strings + colour tones
    format.ts          # ns / Duration presentation transforms
  hooks/useSession.ts  # subscribes to the live stream, exposes {snapshot, error}
  components/          # dashboard UI (SessionHeader, RightPanel, TrackMap, ...)
```

The track map, corner detail, telemetry graphs, and coaching card currently render sample
data or placeholders; they become live in later iterations as the backend surfaces the
supporting data (track geometry, per-corner analysis, coaching).
