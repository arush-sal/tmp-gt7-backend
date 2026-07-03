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
	if cfg.PS5IP == "" {
		return Config{}, errors.New("config: ps5.ip is required")
	}
	return cfg, nil
}
