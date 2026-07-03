package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestConfigDefaults checks the built-in defaults resolve when only the required ps5.ip is given.
func TestConfigDefaults(t *testing.T) {
	c, err := Load([]string{"--ps5.ip=1.2.3.4"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.PS5IP != "1.2.3.4" || c.RecvPort != 33740 || c.SendPort != 33739 {
		t.Errorf("defaults wrong: %+v", c)
	}
	if c.HeartbeatInterval != time.Second || c.LogLevel != "info" || c.LiveBufferSize != 1024 {
		t.Errorf("defaults wrong: %+v", c)
	}
}

// TestConfigRequiresPS5IP checks Load errors when the required console IP is absent.
func TestConfigRequiresPS5IP(t *testing.T) {
	if _, err := Load(nil); err == nil {
		t.Error("Load with no ps5.ip should error")
	}
}

// TestConfigPrecedence checks the flag > env > file > default ladder for one representative key.
func TestConfigPrecedence(t *testing.T) {
	// default (no override)
	if c, _ := Load([]string{"--ps5.ip=x"}); c.RecvPort != 33740 {
		t.Errorf("default: RecvPort = %d, want 33740", c.RecvPort)
	}

	// file beats default
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("udp:\n  recv_port: 43000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load([]string{"--ps5.ip=x", "--config=" + path}); c.RecvPort != 43000 {
		t.Errorf("file: RecvPort = %d, want 43000", c.RecvPort)
	}

	// env beats file
	t.Setenv("GT7_UDP_RECV_PORT", "44000")
	if c, _ := Load([]string{"--ps5.ip=x", "--config=" + path}); c.RecvPort != 44000 {
		t.Errorf("env-over-file: RecvPort = %d, want 44000", c.RecvPort)
	}

	// flag beats env
	if c, _ := Load([]string{"--ps5.ip=x", "--config=" + path, "--udp.recv_port=42000"}); c.RecvPort != 42000 {
		t.Errorf("flag-over-env: RecvPort = %d, want 42000", c.RecvPort)
	}
}
