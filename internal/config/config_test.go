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

// TestConfigValidation checks Load rejects runtime-unsafe values (which would otherwise panic or
// spin) and accepts a fully valid set.
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"valid", []string{"--ps5.ip=1.2.3.4"}, false},
		{"zero heartbeat", []string{"--ps5.ip=x", "--heartbeat.interval=0"}, true},
		{"negative heartbeat", []string{"--ps5.ip=x", "--heartbeat.interval=-1s"}, true},
		{"zero live buffer", []string{"--ps5.ip=x", "--live_buffer.size=0"}, true},
		{"zero raw buffer", []string{"--ps5.ip=x", "--raw_buffer.size=0"}, true},
		{"recv port zero", []string{"--ps5.ip=x", "--udp.recv_port=0"}, true},
		{"recv port too high", []string{"--ps5.ip=x", "--udp.recv_port=70000"}, true},
		{"send port zero", []string{"--ps5.ip=x", "--udp.send_port=0"}, true},
		{"empty rtps addr", []string{"--ps5.ip=x", "--rtps.addr="}, true},
		{"bad log level", []string{"--ps5.ip=x", "--log.level=trace"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load(%v) err = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
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

// TestLoadBackend_Defaults checks the backend binary's zero-flag defaults.
func TestLoadBackend_Defaults(t *testing.T) {
	c, err := LoadBackend(nil)
	if err != nil {
		t.Fatalf("LoadBackend: %v", err)
	}
	if c.IngestAddr != "localhost:50051" {
		t.Errorf("IngestAddr = %q, want localhost:50051", c.IngestAddr)
	}
	if c.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", c.LogLevel)
	}
}

// TestLoadBackend_Overrides checks flag and env precedence for ingest.addr (flag > env).
func TestLoadBackend_Overrides(t *testing.T) {
	t.Setenv("GT7_INGEST_ADDR", "0.0.0.0:6000")
	if c, _ := LoadBackend(nil); c.IngestAddr != "0.0.0.0:6000" {
		t.Errorf("env: IngestAddr = %q, want 0.0.0.0:6000", c.IngestAddr)
	}
	if c, _ := LoadBackend([]string{"--ingest.addr=127.0.0.1:7000"}); c.IngestAddr != "127.0.0.1:7000" {
		t.Errorf("flag-over-env: IngestAddr = %q, want 127.0.0.1:7000", c.IngestAddr)
	}
}

// TestLoadBackend_Validation rejects an empty address and an unknown log level.
func TestLoadBackend_Validation(t *testing.T) {
	if _, err := LoadBackend([]string{"--ingest.addr="}); err == nil {
		t.Error("empty ingest.addr: want error, got nil")
	}
	if _, err := LoadBackend([]string{"--log.level=trace"}); err == nil {
		t.Error("bad log.level: want error, got nil")
	}
}

// TestLoadBackend_Storage checks the storage selector + its DSN requirement.
func TestLoadBackend_Storage(t *testing.T) {
	// default is memory, no DSN needed
	if c, err := LoadBackend(nil); err != nil || c.Storage != "memory" {
		t.Fatalf("default storage: c=%+v err=%v, want memory/nil", c, err)
	}
	// postgres without a DSN is rejected
	if _, err := LoadBackend([]string{"--storage=postgres"}); err == nil {
		t.Error("storage=postgres without db.dsn: want error, got nil")
	}
	// postgres with a DSN is accepted
	if c, err := LoadBackend([]string{"--storage=postgres", "--db.dsn=postgres://x"}); err != nil || c.DBDSN != "postgres://x" {
		t.Errorf("storage=postgres with dsn: c=%+v err=%v", c, err)
	}
	// unknown backing is rejected
	if _, err := LoadBackend([]string{"--storage=redis"}); err == nil {
		t.Error("storage=redis: want error, got nil")
	}
}

// TestLoadBackend_APIAddr checks the api.addr default + required validation.
func TestLoadBackend_APIAddr(t *testing.T) {
	if c, err := LoadBackend(nil); err != nil || c.APIAddr != "localhost:8080" {
		t.Fatalf("default api.addr: c=%+v err=%v, want localhost:8080", c, err)
	}
	if _, err := LoadBackend([]string{"--api.addr="}); err == nil {
		t.Error("empty api.addr: want error, got nil")
	}
}

// TestConfig_ReloadDiffClassification checks ClassifyBackendReload/ClassifyCollectorReload bucket
// each changed field into either the live-appliable set (log.level [+ heartbeat.interval for the
// collector]) or the restart-required set (everything else).
func TestConfig_ReloadDiffClassification(t *testing.T) {
	old := BackendConfig{APIAddr: "a", LogLevel: "info"}
	next := BackendConfig{APIAddr: "b", LogLevel: "debug"}
	live, restart := ClassifyBackendReload(old, next)
	if _, ok := live["log.level"]; !ok {
		t.Error("log.level should be live-appliable")
	}
	if _, ok := restart["api.addr"]; !ok {
		t.Error("api.addr change should be restart-required")
	}
	if len(live) != 1 {
		t.Errorf("live = %v, want exactly log.level", live)
	}
	if len(restart) != 1 {
		t.Errorf("restart = %v, want exactly api.addr", restart)
	}
}

// TestConfig_ReloadDiffClassification_BackendNoChange checks that unchanged fields produce no
// entries at all.
func TestConfig_ReloadDiffClassification_BackendNoChange(t *testing.T) {
	c := BackendConfig{APIAddr: "a", LogLevel: "info", IngestAddr: "x", Storage: "memory"}
	live, restart := ClassifyBackendReload(c, c)
	if len(live) != 0 || len(restart) != 0 {
		t.Errorf("no-change: live=%v restart=%v, want both empty", live, restart)
	}
}

// TestConfig_ReloadDiffClassification_BackendAllRestartFields checks every restart-required
// BackendConfig field is bucketed correctly when changed.
func TestConfig_ReloadDiffClassification_BackendAllRestartFields(t *testing.T) {
	old := BackendConfig{IngestAddr: "a", APIAddr: "a", Storage: "memory", DBDSN: "a"}
	next := BackendConfig{IngestAddr: "b", APIAddr: "b", Storage: "postgres", DBDSN: "b"}
	_, restart := ClassifyBackendReload(old, next)
	for _, key := range []string{"ingest.addr", "api.addr", "storage", "db.dsn"} {
		if _, ok := restart[key]; !ok {
			t.Errorf("restart missing key %q: %v", key, restart)
		}
	}
}

// TestConfig_ReloadDiffClassification_Collector checks the collector's heartbeat.interval is
// live-appliable while everything else (including log.level, which is live too) is bucketed
// correctly, and unrelated changes land in restart.
func TestConfig_ReloadDiffClassification_Collector(t *testing.T) {
	old := Config{PS5IP: "1.1.1.1", RecvPort: 33740, LogLevel: "info", HeartbeatInterval: time.Second}
	next := Config{PS5IP: "1.1.1.1", RecvPort: 33741, LogLevel: "debug", HeartbeatInterval: 2 * time.Second}
	live, restart := ClassifyCollectorReload(old, next)
	if _, ok := live["log.level"]; !ok {
		t.Error("log.level should be live-appliable")
	}
	if _, ok := live["heartbeat.interval"]; !ok {
		t.Error("heartbeat.interval should be live-appliable for the collector")
	}
	if _, ok := restart["udp.recv_port"]; !ok {
		t.Error("udp.recv_port change should be restart-required")
	}
	if len(live) != 2 {
		t.Errorf("live = %v, want exactly log.level + heartbeat.interval", live)
	}
	if len(restart) != 1 {
		t.Errorf("restart = %v, want exactly udp.recv_port", restart)
	}
}

// TestConfig_ReloadDiffClassification_CollectorAllRestartFields checks every restart-required
// collector Config field is bucketed correctly when changed.
func TestConfig_ReloadDiffClassification_CollectorAllRestartFields(t *testing.T) {
	old := Config{
		PS5IP: "1.1.1.1", RecvPort: 1, SendPort: 1, RTPSAddr: "a",
		RawDumpEnabled: false, RawDumpDir: "a", LiveBufferSize: 1, RawBufferSize: 1,
	}
	next := Config{
		PS5IP: "2.2.2.2", RecvPort: 2, SendPort: 2, RTPSAddr: "b",
		RawDumpEnabled: true, RawDumpDir: "b", LiveBufferSize: 2, RawBufferSize: 2,
	}
	_, restart := ClassifyCollectorReload(old, next)
	for _, key := range []string{
		"ps5.ip", "udp.recv_port", "udp.send_port", "rtps.addr",
		"rawdump.enabled", "rawdump.dir", "live_buffer.size", "raw_buffer.size",
	} {
		if _, ok := restart[key]; !ok {
			t.Errorf("restart missing key %q: %v", key, restart)
		}
	}
}

// TestWatch_InvokesOnChangeOnFileWrite checks that Watch fires onChange when the watched config
// file is rewritten. It polls with a timeout because fsnotify delivery is asynchronous.
func TestWatch_InvokesOnChangeOnFileWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("log:\n  level: info\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fired := make(chan struct{}, 1)
	stop, err := Watch(path, func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer stop()

	// Give the watcher a moment to start, then rewrite the file.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(path, []byte("log:\n  level: debug\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("onChange was not invoked after config file write")
	}
}
