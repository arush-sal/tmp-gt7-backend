# Configuration

Options can be set three ways, in order of precedence (highest first):

1. **Command-line flags** — e.g. `--ps5.ip 192.168.1.50`
2. **Environment variables** — prefix `GT7_`, dots become underscores, e.g. `GT7_PS5_IP`
3. **A config file** — via `--config path/to/config.yaml` (YAML, JSON, or TOML)

Anything not set falls back to its default. A flag beats an environment variable, which beats the
config file, which beats the default.

## Options

| Flag | Env | Default | Description |
|---|---|---|---|
| `--ps5.ip` | `GT7_PS5_IP` | *(required)* | Console IP address. The keep-alive is sent here. |
| `--udp.recv_port` | `GT7_UDP_RECV_PORT` | `33740` | Local UDP port telemetry is received on. |
| `--udp.send_port` | `GT7_UDP_SEND_PORT` | `33739` | Console UDP port the keep-alive is sent to. |
| `--rtps.addr` | `GT7_RTPS_ADDR` | `localhost:50051` | gRPC address of the processing system. |
| `--heartbeat.interval` | `GT7_HEARTBEAT_INTERVAL` | `1s` | How often the keep-alive is sent. |
| `--rawdump.enabled` | `GT7_RAWDUMP_ENABLED` | `false` | Record raw packets to disk (debugging). |
| `--rawdump.dir` | `GT7_RAWDUMP_DIR` | `rawdump` | Directory for raw-packet recordings. |
| `--live_buffer.size` | `GT7_LIVE_BUFFER_SIZE` | `1024` | Outbound frame buffer depth. |
| `--raw_buffer.size` | `GT7_RAW_BUFFER_SIZE` | `1024` | Raw-dump buffer depth. |
| `--log.level` | `GT7_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, or `error`. |
| `--config` | — | *(none)* | Path to a config file. |

## Examples

Flags:

```bash
./collector --ps5.ip 192.168.1.50 --rtps.addr rtps.local:50051 --log.level debug
```

Environment variables:

```bash
export GT7_PS5_IP=192.168.1.50
export GT7_LOG_LEVEL=debug
./collector
```

Config file (`config.yaml`):

```yaml
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

## Notes on buffers

The outbound and raw-dump buffers are intentionally bounded and lossy: if the processing system or
the disk can't keep up, the collector drops data (counted in the periodic statistics) rather than
stalling reception. This keeps the receive path from ever blocking, so no telemetry is lost at the
network layer.
