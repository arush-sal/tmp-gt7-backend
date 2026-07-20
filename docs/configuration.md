# Configuration

Both the collector and the backend are configured the same way. Options can be set three ways, in
order of precedence (highest first):

1. **Command-line flags** — e.g. `--ps5.ip 192.168.1.50`
2. **Environment variables** — prefix `GT7_`, dots become underscores, e.g. `GT7_PS5_IP`
3. **A config file** — via `--config path/to/config.yaml` (YAML, JSON, or TOML)

Anything not set falls back to its default. A flag beats an environment variable, which beats the
config file, which beats the default.

## Collector options (`collector`)

| Flag | Env | Default | Description |
|---|---|---|---|
| `--ps5.ip` | `GT7_PS5_IP` | *(required)* | Console IP address. The keep-alive is sent here. |
| `--udp.recv_port` | `GT7_UDP_RECV_PORT` | `33740` | Local UDP port telemetry is received on. |
| `--udp.send_port` | `GT7_UDP_SEND_PORT` | `33739` | Console UDP port the keep-alive is sent to. |
| `--rtps.addr` | `GT7_RTPS_ADDR` | `localhost:50051` | gRPC address of the backend's ingest server. |
| `--heartbeat.interval` | `GT7_HEARTBEAT_INTERVAL` | `1s` | How often the keep-alive is sent. |
| `--rawdump.enabled` | `GT7_RAWDUMP_ENABLED` | `false` | Record raw packets to disk (debugging). |
| `--rawdump.dir` | `GT7_RAWDUMP_DIR` | `rawdump` | Directory for raw-packet recordings. |
| `--live_buffer.size` | `GT7_LIVE_BUFFER_SIZE` | `1024` | Outbound frame buffer depth. |
| `--raw_buffer.size` | `GT7_RAW_BUFFER_SIZE` | `1024` | Raw-dump buffer depth. |
| `--log.level` | `GT7_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error`. |
| `--config` | — | *(none)* | Path to a config file. |

## Backend options (`rtps`)

| Flag | Env | Default | Description |
|---|---|---|---|
| `--ingest.addr` | `GT7_INGEST_ADDR` | `localhost:50051` | Address the gRPC telemetry ingest server listens on. |
| `--api.addr` | `GT7_API_ADDR` | `localhost:8080` | Address the HTTP read API listens on. |
| `--storage` | `GT7_STORAGE` | `memory` | Session store backing: `memory` or `postgres`. |
| `--db.dsn` | `GT7_DB_DSN` | *(empty)* | PostgreSQL connection string. Required when `--storage postgres`. |
| `--log.level` | `GT7_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error`. |
| `--config` | — | *(none)* | Path to a config file. |

The Reference Knowledge Base database path (`$HOME/.garage/rkb.db`) is a fixed convention, **not** a
configuration key. See [tools/rkb](tools/rkb.md).

### Config reload

When the backend is started with `--config`, it watches the file and applies changes to `log.level`
live. Every other key (`ingest.addr`, `api.addr`, `storage`, `db.dsn`) requires a restart; a change
to one of those is logged as "requires restart" rather than applied.

## Examples

Collector flags:

```bash
./collector --ps5.ip 192.168.1.50 --rtps.addr rtps.local:50051 --log.level debug
```

Backend with Postgres:

```bash
./rtps --storage postgres --db.dsn 'postgres://user:pass@localhost:5432/garage?sslmode=disable'
```

Environment variables:

```bash
export GT7_PS5_IP=192.168.1.50
export GT7_LOG_LEVEL=debug
./collector
```

Config file (`config.yaml`):

```yaml
# collector
ps5:
  ip: 192.168.1.50
rtps:
  addr: rtps.local:50051
log:
  level: info
```

```bash
./collector --config config.yaml
```

## Notes on the collector's buffers

The collector's outbound and raw-dump buffers are intentionally bounded and lossy: if the backend or
the disk can't keep up, the collector drops data (counted in the periodic statistics) rather than
stalling reception. This keeps the receive path from ever blocking, so no telemetry is lost at the
network layer.
