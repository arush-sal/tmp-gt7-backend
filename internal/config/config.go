package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config holds the collector's runtime configuration, resolved with precedence flag > env > file >
// default (viper's native order). Environment variables use the GT7_ prefix with dots replaced by
// underscores, e.g. GT7_UDP_RECV_PORT for udp.recv_port.
type Config struct {
	PS5IP             string
	RecvPort          int
	SendPort          int
	RTPSAddr          string
	HeartbeatInterval time.Duration
	RawDumpEnabled    bool
	RawDumpDir        string
	LiveBufferSize    int
	RawBufferSize     int
	LogLevel          string
}

// Load parses args, layers environment variables and an optional config file (via --config), and
// returns the resolved Config. ps5.ip is required. Precedence is flag > env > file > default.
func Load(args []string) (Config, error) {
	fs := pflag.NewFlagSet("collector", pflag.ContinueOnError)
	fs.String("ps5.ip", "", "GT7 console IP address (required)")
	fs.Int("udp.recv_port", 33740, "UDP port telemetry is received on")
	fs.Int("udp.send_port", 33739, "UDP port the heartbeat is sent to")
	fs.String("rtps.addr", "localhost:50051", "RTPS gRPC address")
	fs.Duration("heartbeat.interval", time.Second, "heartbeat send interval")
	fs.Bool("rawdump.enabled", false, "enable the raw packet debug dump")
	fs.String("rawdump.dir", "rawdump", "directory for raw packet dumps")
	fs.Int("live_buffer.size", 1024, "outbound canonical-frame buffer depth")
	fs.Int("raw_buffer.size", 1024, "raw-dump buffer depth")
	fs.String("log.level", "info", "log level: debug|info|warn|error")
	fs.String("config", "", "path to a config file (optional)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	v := viper.New()
	if err := v.BindPFlags(fs); err != nil {
		return Config{}, err
	}
	v.SetEnvPrefix("GT7")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if path, _ := fs.GetString("config"); path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("config: read %s: %w", path, err)
		}
	}

	cfg := Config{
		PS5IP:             v.GetString("ps5.ip"),
		RecvPort:          v.GetInt("udp.recv_port"),
		SendPort:          v.GetInt("udp.send_port"),
		RTPSAddr:          v.GetString("rtps.addr"),
		HeartbeatInterval: v.GetDuration("heartbeat.interval"),
		RawDumpEnabled:    v.GetBool("rawdump.enabled"),
		RawDumpDir:        v.GetString("rawdump.dir"),
		LiveBufferSize:    v.GetInt("live_buffer.size"),
		RawBufferSize:     v.GetInt("raw_buffer.size"),
		LogLevel:          v.GetString("log.level"),
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate rejects resolved values that would crash or misbehave at runtime: a non-positive
// heartbeat interval panics time.NewTicker, a zero-size buffer makes the publisher spin, and
// out-of-range ports never bind.
func (c Config) validate() error {
	if c.PS5IP == "" {
		return errors.New("config: ps5.ip is required")
	}
	if c.RTPSAddr == "" {
		return errors.New("config: rtps.addr is required")
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
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: log.level must be debug|info|warn|error, got %q", c.LogLevel)
	}
	return nil
}

// validPort reports whether p is a usable TCP/UDP port.
func validPort(p int) bool { return p >= 1 && p <= 65535 }
