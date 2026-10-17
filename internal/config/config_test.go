package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// src is the minimal valid source selection (exactly one of ps5.ip / replay-file is required).
var src = []string{"--ps5.ip=1.2.3.4"}

// TestLoadBackend_Defaults checks the backend binary's zero-flag defaults.
func TestLoadBackend_Defaults(t *testing.T) {
	c, err := LoadBackend(src)
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
	if c, _ := LoadBackend(src); c.IngestAddr != "0.0.0.0:6000" {
		t.Errorf("env: IngestAddr = %q, want 0.0.0.0:6000", c.IngestAddr)
	}
	if c, _ := LoadBackend([]string{"--ps5.ip=1.2.3.4", "--ingest.addr=127.0.0.1:7000"}); c.IngestAddr != "127.0.0.1:7000" {
		t.Errorf("flag-over-env: IngestAddr = %q, want 127.0.0.1:7000", c.IngestAddr)
	}
}

// TestLoadBackend_Validation rejects an empty address and an unknown log level.
func TestLoadBackend_Validation(t *testing.T) {
	if _, err := LoadBackend([]string{"--ps5.ip=1.2.3.4", "--ingest.addr="}); err == nil {
		t.Error("empty ingest.addr: want error, got nil")
	}
	if _, err := LoadBackend([]string{"--ps5.ip=1.2.3.4", "--log.level=trace"}); err == nil {
		t.Error("bad log.level: want error, got nil")
	}
}

// TestLoadBackend_Storage checks the storage selector + its DSN requirement.
func TestLoadBackend_Storage(t *testing.T) {
	// default is memory, no DSN needed
	if c, err := LoadBackend(src); err != nil || c.Storage != "memory" {
		t.Fatalf("default storage: c=%+v err=%v, want memory/nil", c, err)
	}
	// postgres without a DSN is rejected
	if _, err := LoadBackend([]string{"--ps5.ip=1.2.3.4", "--storage=postgres"}); err == nil {
		t.Error("storage=postgres without db.dsn: want error, got nil")
	}
	// postgres with a DSN is accepted
	if c, err := LoadBackend([]string{"--ps5.ip=1.2.3.4", "--storage=postgres", "--db.dsn=postgres://x"}); err != nil || c.DBDSN != "postgres://x" {
		t.Errorf("storage=postgres with dsn: c=%+v err=%v", c, err)
	}
	// unknown backing is rejected
	if _, err := LoadBackend([]string{"--ps5.ip=1.2.3.4", "--storage=redis"}); err == nil {
		t.Error("storage=redis: want error, got nil")
	}
}

// TestLoadBackend_APIAddr checks the api.addr default + required validation.
func TestLoadBackend_APIAddr(t *testing.T) {
	if c, err := LoadBackend(src); err != nil || c.APIAddr != "localhost:8080" {
		t.Fatalf("default api.addr: c=%+v err=%v, want localhost:8080", c, err)
	}
	if _, err := LoadBackend([]string{"--ps5.ip=1.2.3.4", "--api.addr="}); err == nil {
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

// TestLoadBackend_CollectorDefaults checks the in-process collector keys keep their old names and
// defaults.
func TestLoadBackend_CollectorDefaults(t *testing.T) {
	c, err := LoadBackend(src)
	if err != nil {
		t.Fatalf("LoadBackend: %v", err)
	}
	if c.PS5IP != "1.2.3.4" || c.RecvPort != 33740 || c.SendPort != 33739 {
		t.Errorf("defaults wrong: %+v", c)
	}
	if c.HeartbeatInterval != time.Second || c.LiveBufferSize != 1024 || c.RawBufferSize != 1024 {
		t.Errorf("defaults wrong: %+v", c)
	}
	if c.RawDumpEnabled || c.RawDumpDir != "rawdump" || c.ReplaySpeed != 1.0 || c.ReplayFile != "" {
		t.Errorf("defaults wrong: %+v", c)
	}
}

// TestLoadBackend_SourceSelection: exactly one of ps5.ip / replay-file is required — neither leaves
// rtps with no telemetry source, and both would mix a replay into live PB/session state.
func TestLoadBackend_SourceSelection(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"neither", nil, true},
		{"both", []string{"--ps5.ip=1.2.3.4", "--replay-file=x.pcapng"}, true},
		{"ps5 only", []string{"--ps5.ip=1.2.3.4"}, false},
		{"replay only", []string{"--replay-file=x.pcapng"}, false},
		{"replay speed", []string{"--replay-file=x.pcapng", "--replay.speed=16"}, false},
		{"replay speed zero", []string{"--replay-file=x.pcapng", "--replay.speed=0"}, true},
		{"replay speed negative", []string{"--replay-file=x.pcapng", "--replay.speed=-1"}, true},
		{"replay speed NaN", []string{"--replay-file=x.pcapng", "--replay.speed=NaN"}, true},
		{"replay speed Inf", []string{"--replay-file=x.pcapng", "--replay.speed=+Inf"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadBackend(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadBackend(%v) err = %v, wantErr %v", tt.args, err, tt.wantErr)
			}
		})
	}
}

// TestLoadBackend_ReplayKeys: --replay-file is the flag; replay.file is its file/env key.
func TestLoadBackend_ReplayKeys(t *testing.T) {
	c, err := LoadBackend([]string{"--replay-file=a.pcapng", "--replay.speed=4"})
	if err != nil || c.ReplayFile != "a.pcapng" || c.ReplaySpeed != 4 {
		t.Fatalf("flags: c=%+v err=%v", c, err)
	}
	t.Setenv("GT7_REPLAY_FILE", "env.pcapng")
	if c, err := LoadBackend(nil); err != nil || c.ReplayFile != "env.pcapng" {
		t.Errorf("env: c=%+v err=%v", c, err)
	}
	t.Setenv("GT7_REPLAY_FILE", "")
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("replay:\n  file: file.pcapng\n  speed: 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := LoadBackend([]string{"--config=" + path}); err != nil || c.ReplayFile != "file.pcapng" || c.ReplaySpeed != 8 {
		t.Errorf("file: c=%+v err=%v", c, err)
	}
}

// TestLoadBackend_CollectorValidation rejects runtime-unsafe collector values (which would otherwise
// panic or spin).
func TestLoadBackend_CollectorValidation(t *testing.T) {
	for _, arg := range []string{
		"--heartbeat.interval=0", "--heartbeat.interval=-1s", "--live_buffer.size=0", "--raw_buffer.size=0",
		"--udp.recv_port=0", "--udp.recv_port=70000", "--udp.send_port=0",
	} {
		if _, err := LoadBackend([]string{"--ps5.ip=1.2.3.4", arg}); err == nil {
			t.Errorf("LoadBackend(%s): want error, got nil", arg)
		}
	}
}

// TestLoadBackend_Precedence checks the flag > env > file > default ladder for one collector key.
func TestLoadBackend_Precedence(t *testing.T) {
	if c, _ := LoadBackend(src); c.RecvPort != 33740 {
		t.Errorf("default: RecvPort = %d, want 33740", c.RecvPort)
	}
	path := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(path, []byte("udp:\n  recv_port: 43000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, _ := LoadBackend(append(src, "--config="+path)); c.RecvPort != 43000 {
		t.Errorf("file: RecvPort = %d, want 43000", c.RecvPort)
	}
	t.Setenv("GT7_UDP_RECV_PORT", "44000")
	if c, _ := LoadBackend(append(src, "--config="+path)); c.RecvPort != 44000 {
		t.Errorf("env-over-file: RecvPort = %d, want 44000", c.RecvPort)
	}
	if c, _ := LoadBackend(append(src, "--config="+path, "--udp.recv_port=42000")); c.RecvPort != 42000 {
		t.Errorf("flag-over-env: RecvPort = %d, want 42000", c.RecvPort)
	}
}

// TestConfig_ReloadDiffClassification_Collector: heartbeat.interval is live-appliable; every other
// collector/replay key needs a restart.
func TestConfig_ReloadDiffClassification_Collector(t *testing.T) {
	old := BackendConfig{
		PS5IP: "1.1.1.1", RecvPort: 1, SendPort: 1, HeartbeatInterval: time.Second, RawDumpDir: "a",
		LiveBufferSize: 1, RawBufferSize: 1, ReplayFile: "a", ReplaySpeed: 1,
	}
	next := BackendConfig{
		PS5IP: "2.2.2.2", RecvPort: 2, SendPort: 2, HeartbeatInterval: 2 * time.Second, RawDumpEnabled: true,
		RawDumpDir: "b", LiveBufferSize: 2, RawBufferSize: 2, ReplayFile: "b", ReplaySpeed: 2,
	}
	live, restart := ClassifyBackendReload(old, next)
	if _, ok := live["heartbeat.interval"]; !ok || len(live) != 1 {
		t.Errorf("live = %v, want exactly heartbeat.interval", live)
	}
	for _, key := range []string{
		"ps5.ip", "udp.recv_port", "udp.send_port", "rawdump.enabled", "rawdump.dir",
		"live_buffer.size", "raw_buffer.size", "replay.file", "replay.speed",
	} {
		if _, ok := restart[key]; !ok {
			t.Errorf("restart missing key %q: %v", key, restart)
		}
	}
}

// TestLoadBackend_APIAllowedHosts: api.allowed_hosts is a comma-separated host list from the flag or
// the environment (trimmed, empties dropped); unset by default; changing it requires a restart.
func TestLoadBackend_APIAllowedHosts(t *testing.T) {
	if c, err := LoadBackend(src); err != nil || len(c.APIAllowedHosts) != 0 {
		t.Fatalf("default: %v %v, want none", c.APIAllowedHosts, err)
	}
	c, err := LoadBackend(append([]string{"--api.allowed_hosts=garage.lan, 192.168.1.5"}, src...))
	if err != nil || len(c.APIAllowedHosts) != 2 || c.APIAllowedHosts[0] != "garage.lan" || c.APIAllowedHosts[1] != "192.168.1.5" {
		t.Fatalf("flag: %q %v", c.APIAllowedHosts, err)
	}
	t.Setenv("GT7_API_ALLOWED_HOSTS", "a.lan,,b.lan ")
	c, err = LoadBackend(src)
	if err != nil || len(c.APIAllowedHosts) != 2 || c.APIAllowedHosts[0] != "a.lan" || c.APIAllowedHosts[1] != "b.lan" {
		t.Fatalf("env: %q %v", c.APIAllowedHosts, err)
	}
	_, restart := ClassifyBackendReload(BackendConfig{}, BackendConfig{APIAllowedHosts: []string{"x"}})
	if _, ok := restart["api.allowed_hosts"]; !ok {
		t.Errorf("api.allowed_hosts change should be restart-required: %v", restart)
	}
}
