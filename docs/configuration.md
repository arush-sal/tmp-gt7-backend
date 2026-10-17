# Configuration

`rtps` (the backend with its in-process collector) is configured three ways, in
order of precedence (highest first):

1. **Command-line flags** — e.g. `--ps5.ip 192.168.1.50`
2. **Environment variables** — prefix `GT7_`, dots become underscores, e.g. `GT7_PS5_IP`
3. **A config file** — via `--config path/to/config.yaml` (YAML, JSON, or TOML)

Anything not set falls back to its default. A flag beats an environment variable, which beats the
config file, which beats the default.

## Telemetry source

Exactly one of these is required; passing neither or both is an error.

| Flag | Env | Default | Description |
|---|---|---|---|
| `--ps5.ip` | `GT7_PS5_IP` | *(none)* | Console IP address: run the GT7 UDP collector. The keep-alive is sent here. |
| `--replay-file` | `GT7_REPLAY_FILE` | *(none)* | Replay this `.pcap`/`.pcapng` capture instead. After it ends, the session is finalized and `rtps` keeps serving. Config-file key: `replay.file`. |
| `--replay.speed` | `GT7_REPLAY_SPEED` | `1` | Replay pacing multiplier over the original capture cadence (must be > 0). |

## Collector options

| Flag | Env | Default | Description |
|---|---|---|---|
| `--udp.recv_port` | `GT7_UDP_RECV_PORT` | `33740` | Local UDP port telemetry is received on. |
| `--udp.send_port` | `GT7_UDP_SEND_PORT` | `33739` | Console UDP port the keep-alive is sent to. |
| `--heartbeat.interval` | `GT7_HEARTBEAT_INTERVAL` | `1s` | How often the keep-alive is sent. |
| `--rawdump.enabled` | `GT7_RAWDUMP_ENABLED` | `false` | Record raw packets to disk (debugging). |
| `--rawdump.dir` | `GT7_RAWDUMP_DIR` | `rawdump` | Directory for raw-packet recordings. |
| `--live_buffer.size` | `GT7_LIVE_BUFFER_SIZE` | `1024` | Frame buffer depth between the collector (or replay) and ingest. |
| `--raw_buffer.size` | `GT7_RAW_BUFFER_SIZE` | `1024` | Raw-dump buffer depth. |

## Backend options

| Flag | Env | Default | Description |
|---|---|---|---|
| `--ingest.addr` | `GT7_INGEST_ADDR` | `localhost:50051` | Address the gRPC telemetry ingest server listens on (for external collectors and the replay-client). |
| `--api.addr` | `GT7_API_ADDR` | `localhost:8080` | Address the HTTP read API listens on. |
| `--api.allowed_hosts` | `GT7_API_ALLOWED_HOSTS` | *(empty)* | Comma-separated extra host names or IPs (such as `192.168.1.50,garage.lan`) allowed to call the API's mutating endpoints. `localhost`, `127.0.0.1`, `::1` and the host of `--api.addr` (unless it is `0.0.0.0` or `::`) are always allowed. Set it when you bind `0.0.0.0` and open the dashboard by LAN IP or hostname; it is never filled in automatically. See [Security](api.md#security). |
| `--storage` | `GT7_STORAGE` | `memory` | Session store backing: `memory` or `postgres`. |
| `--db.dsn` | `GT7_DB_DSN` | *(empty)* | PostgreSQL connection string. Required when `--storage postgres`. |
| `--log.level` | `GT7_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error`. |
| `--config` | — | *(none)* | Path to a config file. |

The Reference Knowledge Base database path (`$HOME/.garage/rkb.db`) is a fixed convention, **not** a
configuration key. See [tools/rkb](tools/rkb.md).

### Config reload

When `rtps` is started with `--config`, it watches the file and applies changes to `log.level` and
`heartbeat.interval` live. Every other key requires a restart; a change to one of those is logged as
"requires restart" rather than applied.

## Examples

Live, with Postgres:

```bash
./rtps --ps5.ip 192.168.1.50 --storage postgres --db.dsn 'postgres://user:pass@localhost:5432/garage?sslmode=disable'
```

Environment variables:

```bash
export GT7_PS5_IP=192.168.1.50
export GT7_LOG_LEVEL=debug
./rtps
```

Config file (`config.yaml`):

```yaml
ps5:
  ip: 192.168.1.50
log:
  level: info
```

```bash
./rtps --config config.yaml
```

## Notes on the collector's buffers

The collector's frame and raw-dump buffers are intentionally bounded and lossy: if processing or the
disk can't keep up, the collector drops the oldest data (counted in the periodic statistics) rather than
stalling reception. This keeps the receive path from ever blocking, so no telemetry is lost at the
network layer.
