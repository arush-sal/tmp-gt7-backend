package config

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// validPort reports whether p is a usable TCP/UDP port.
func validPort(p int) bool { return p >= 1 && p <= 65535 }

// BackendConfig holds the rtps binary's runtime configuration: the backend (ingest, API, storage)
// plus the in-process telemetry source — either the GT7 UDP collector (ps5.ip) or a capture replay
// (replay-file). Precedence is flag > env > file > default (viper's native order); environment
// variables use the GT7_ prefix with dots replaced by underscores (e.g. GT7_UDP_RECV_PORT for
// udp.recv_port, GT7_REPLAY_FILE for replay.file).
type BackendConfig struct {
	// IngestAddr is the TCP address the telemetry ingest gRPC server listens on, for external
	// collectors and the replay-client. Defaults to localhost:50051.
	IngestAddr string
	// APIAddr is the TCP address the HTTP read API (sessions/coaching) listens on.
	APIAddr string
	// APIAllowedHosts are extra Host names (beyond localhost, 127.0.0.1, ::1 and api.addr's own host)
	// the mutating API routes accept - needed when api.addr binds 0.0.0.0 and the dashboard is opened
	// by LAN IP or hostname. Never inferred (see api.MutationHosts).
	APIAllowedHosts []string
	LogLevel        string

	// Storage selects the session store backing: "memory" (default; dev/tests) or "postgres".
	Storage string
	// DBDSN is the PostgreSQL connection string; required when Storage is "postgres".
	DBDSN string

	// PS5IP is the console address; when set, rtps runs the GT7 UDP collector in-process.
	PS5IP string
	// RecvPort is the UDP port telemetry is received on; SendPort is where the heartbeat is sent.
	RecvPort int
	SendPort int
	// HeartbeatInterval is the keep-alive send interval (applies live on config reload).
	HeartbeatInterval time.Duration
	// RawDumpEnabled/RawDumpDir/RawBufferSize control the debug-only raw packet dump.
	RawDumpEnabled bool
	RawDumpDir     string
	RawBufferSize  int
	// LiveBufferSize is the depth of the bounded, drop-oldest buffer between the collector (or replay)
	// and the in-process ingest.
	LiveBufferSize int

	// ReplayFile, when set, makes rtps replay this .pcap/.pcapng capture in-process instead of running
	// the UDP collector. Mutually exclusive with PS5IP.
	ReplayFile string
	// ReplaySpeed scales replay pacing relative to the original capture cadence (2 = twice as fast).
	ReplaySpeed float64

	// ConfigPath is the --config file path used to resolve this BackendConfig, if any. Empty means
	// no config file was given — callers use it to decide whether to start a hot-reload Watch.
	ConfigPath string
}

// LoadBackend parses args for the rtps binary and returns the resolved BackendConfig. Exactly one of
// --ps5.ip / --replay-file is required.
func LoadBackend(args []string) (BackendConfig, error) {
	fs := pflag.NewFlagSet("rtps", pflag.ContinueOnError)
	fs.String("ingest.addr", "localhost:50051", "address the telemetry ingest gRPC server listens on")
	fs.String("api.addr", "localhost:8080", "address the HTTP read API listens on")
	fs.StringSlice("api.allowed_hosts", nil, "comma-separated extra hosts allowed to call the mutating API routes (e.g. a LAN IP or hostname)")
	fs.String("log.level", "info", "log level: debug|info|warn|error")
	fs.String("storage", "memory", "session store backing: memory|postgres")
	fs.String("db.dsn", "", "PostgreSQL connection string (required when storage=postgres)")
	fs.String("ps5.ip", "", "GT7 console IP address: run the UDP collector (exclusive with --replay-file)")
	fs.Int("udp.recv_port", 33740, "UDP port telemetry is received on")
	fs.Int("udp.send_port", 33739, "UDP port the heartbeat is sent to")
	fs.Duration("heartbeat.interval", time.Second, "heartbeat send interval")
	fs.Bool("rawdump.enabled", false, "enable the raw packet debug dump")
	fs.String("rawdump.dir", "rawdump", "directory for raw packet dumps")
	fs.Int("live_buffer.size", 1024, "in-process canonical-frame buffer depth")
	fs.Int("raw_buffer.size", 1024, "raw-dump buffer depth")
	fs.String("replay-file", "", "replay this .pcap/.pcapng capture in-process instead of a live PS5 (exclusive with --ps5.ip)")
	fs.Float64("replay.speed", 1.0, "replay pacing multiplier over the original capture cadence (> 0)")
	fs.String("config", "", "path to a config file (optional)")
	if err := fs.Parse(args); err != nil {
		return BackendConfig{}, err
	}

	v := viper.New()
	if err := v.BindPFlags(fs); err != nil {
		return BackendConfig{}, err
	}
	// The user-facing flag is --replay-file, but its key follows the dotted section.key convention
	// (replay.file) so the config file (replay: {file: ...}) and env (GT7_REPLAY_FILE) line up with
	// replay.speed.
	if err := v.BindPFlag("replay.file", fs.Lookup("replay-file")); err != nil {
		return BackendConfig{}, err
	}
	v.SetEnvPrefix("GT7")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	path, _ := fs.GetString("config")
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return BackendConfig{}, fmt.Errorf("config: read %s: %w", path, err)
		}
	}

	cfg := BackendConfig{
		IngestAddr:        v.GetString("ingest.addr"),
		APIAddr:           v.GetString("api.addr"),
		APIAllowedHosts:   hostList(v.GetStringSlice("api.allowed_hosts")),
		LogLevel:          v.GetString("log.level"),
		Storage:           v.GetString("storage"),
		DBDSN:             v.GetString("db.dsn"),
		PS5IP:             v.GetString("ps5.ip"),
		RecvPort:          v.GetInt("udp.recv_port"),
		SendPort:          v.GetInt("udp.send_port"),
		HeartbeatInterval: v.GetDuration("heartbeat.interval"),
		RawDumpEnabled:    v.GetBool("rawdump.enabled"),
		RawDumpDir:        v.GetString("rawdump.dir"),
		LiveBufferSize:    v.GetInt("live_buffer.size"),
		RawBufferSize:     v.GetInt("raw_buffer.size"),
		ReplayFile:        v.GetString("replay.file"),
		ReplaySpeed:       v.GetFloat64("replay.speed"),
		ConfigPath:        path,
	}
	if err := cfg.validate(); err != nil {
		return BackendConfig{}, err
	}
	return cfg, nil
}

// hostList flattens a host list that may arrive as a slice (flag, YAML list) or as one comma-separated
// string (env, YAML scalar - viper does not split those on commas), trimming and dropping empties.
func hostList(raw []string) []string {
	var out []string
	for _, item := range raw {
		for _, h := range strings.Split(item, ",") {
			if h = strings.TrimSpace(h); h != "" {
				out = append(out, h)
			}
		}
	}
	return out
}

// validate rejects resolved values that would fail at runtime: an empty listen address never binds,
// an unknown log level can't build a logger, an unknown storage backing has no implementation, and
// postgres storage needs a DSN to connect. On the collector side, a non-positive heartbeat interval
// panics the timer, a zero-size buffer spins, and out-of-range ports never bind. Exactly one telemetry
// source is required: none leaves rtps idle, and a replay alongside live would pollute live PB and
// session state.
func (c BackendConfig) validate() error {
	if c.IngestAddr == "" {
		return errors.New("config: ingest.addr is required")
	}
	if c.APIAddr == "" {
		return errors.New("config: api.addr is required")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: log.level must be debug|info|warn|error, got %q", c.LogLevel)
	}
	switch c.Storage {
	case "memory":
	case "postgres":
		if c.DBDSN == "" {
			return errors.New("config: db.dsn is required when storage=postgres")
		}
	default:
		return fmt.Errorf("config: storage must be memory|postgres, got %q", c.Storage)
	}
	switch {
	case c.PS5IP == "" && c.ReplayFile == "":
		return errors.New("config: one of --ps5.ip (live) or --replay-file (replay) is required")
	case c.PS5IP != "" && c.ReplayFile != "":
		return errors.New("config: --ps5.ip and --replay-file are mutually exclusive")
	}
	if !(c.ReplaySpeed > 0) || math.IsInf(c.ReplaySpeed, 0) {
		return fmt.Errorf("config: replay.speed must be a finite number > 0, got %v", c.ReplaySpeed)
	}
	if !validPort(c.RecvPort) {
		return fmt.Errorf("config: udp.recv_port must be 1..65535, got %d", c.RecvPort)
	}
	if !validPort(c.SendPort) {
		return fmt.Errorf("config: udp.send_port must be 1..65535, got %d", c.SendPort)
	}
	if c.HeartbeatInterval <= 0 {
		return fmt.Errorf("config: heartbeat.interval must be > 0, got %s", c.HeartbeatInterval)
	}
	if c.LiveBufferSize <= 0 {
		return fmt.Errorf("config: live_buffer.size must be > 0, got %d", c.LiveBufferSize)
	}
	if c.RawBufferSize <= 0 {
		return fmt.Errorf("config: raw_buffer.size must be > 0, got %d", c.RawBufferSize)
	}
	return nil
}

// reloadDiff is the [old, new] string pair recorded for a changed config key, used for logging a
// hot-reload's effect regardless of whether it was applied live or requires a restart.
type reloadDiff = [2]string

// diffField compares old and next (already string-formatted) and, if they differ, records the pair
// under key into the live or restart bucket depending on isLive.
func diffField(live, restart map[string]reloadDiff, key, old, next string, isLive bool) {
	if old == next {
		return
	}
	if isLive {
		live[key] = reloadDiff{old, next}
	} else {
		restart[key] = reloadDiff{old, next}
	}
}

// ClassifyBackendReload compares an old and reloaded BackendConfig and buckets every changed key
// into live (safe to apply without a restart: log.level, heartbeat.interval) or restart (requires a
// process restart to take effect: everything else). It is pure — callers own applying the live
// changes and logging the restart-required ones.
func ClassifyBackendReload(old, next BackendConfig) (live, restart map[string]reloadDiff) {
	live = make(map[string]reloadDiff)
	restart = make(map[string]reloadDiff)

	diffField(live, restart, "log.level", old.LogLevel, next.LogLevel, true)
	diffField(live, restart, "heartbeat.interval", old.HeartbeatInterval.String(), next.HeartbeatInterval.String(), true)
	diffField(live, restart, "ingest.addr", old.IngestAddr, next.IngestAddr, false)
	diffField(live, restart, "api.addr", old.APIAddr, next.APIAddr, false)
	diffField(live, restart, "api.allowed_hosts", strings.Join(old.APIAllowedHosts, ","), strings.Join(next.APIAllowedHosts, ","), false)
	diffField(live, restart, "storage", old.Storage, next.Storage, false)
	diffField(live, restart, "db.dsn", old.DBDSN, next.DBDSN, false)
	diffField(live, restart, "ps5.ip", old.PS5IP, next.PS5IP, false)
	diffField(live, restart, "udp.recv_port", strconv.Itoa(old.RecvPort), strconv.Itoa(next.RecvPort), false)
	diffField(live, restart, "udp.send_port", strconv.Itoa(old.SendPort), strconv.Itoa(next.SendPort), false)
	diffField(live, restart, "rawdump.enabled", strconv.FormatBool(old.RawDumpEnabled), strconv.FormatBool(next.RawDumpEnabled), false)
	diffField(live, restart, "rawdump.dir", old.RawDumpDir, next.RawDumpDir, false)
	diffField(live, restart, "live_buffer.size", strconv.Itoa(old.LiveBufferSize), strconv.Itoa(next.LiveBufferSize), false)
	diffField(live, restart, "raw_buffer.size", strconv.Itoa(old.RawBufferSize), strconv.Itoa(next.RawBufferSize), false)
	diffField(live, restart, "replay.file", old.ReplayFile, next.ReplayFile, false)
	diffField(live, restart, "replay.speed", strconv.FormatFloat(old.ReplaySpeed, 'g', -1, 64), strconv.FormatFloat(next.ReplaySpeed, 'g', -1, 64), false)

	return live, restart
}

// Watch starts watching the config file at path for changes and invokes onChange (with no
// arguments — the caller re-runs LoadBackend and classifies the diff itself) each time the file
// is rewritten. It is a thin wrapper around viper's WatchConfig/OnConfigChange, kept here so the
// only-when-a-config-path-was-given guard lives with the rest of the config package rather than
// being duplicated in main. Callers MUST NOT call Watch with an empty path — a viper instance
// with no configured file still spawns a watcher goroutine and logs a spurious error, so guard the
// call at the call site with `if cfgPath != ""`.
//
// Two caveats callers must know about:
//
//  1. The returned stop func is a NO-OP. Viper exposes no API to stop or release its underlying
//     fsnotify watcher goroutine once WatchConfig has started it, so stop() does not release
//     anything — it exists only so call sites read naturally with `defer stop()` and can adopt real
//     cleanup later if viper ever adds one.
//  2. Viper's watcher goroutine can silently stop delivering events after certain fsnotify sequences
//     — most notably an exact-path Remove event, which is exactly what many editors and config-
//     management tools produce when they save via write-temp-file-then-rename instead of an
//     in-place write. When that happens, hot-reload goes dark with no error and no log line; the
//     process keeps running on whatever config it last loaded. There is no supported way to detect
//     this from outside viper short of noticing that expected reloads have stopped arriving.
//
// Given (2), hot-reload here should be treated as a best-effort convenience, not a guaranteed
// delivery mechanism — operators who need certainty that a config change took effect should still
// restart the process.
func Watch(path string, onChange func()) (stop func(), err error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: watch %s: %w", path, err)
	}
	v.OnConfigChange(func(_ fsnotify.Event) { onChange() })
	v.WatchConfig()
	return func() {}, nil
}
