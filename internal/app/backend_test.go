package app

import (
	"context"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/internal/config"
	"go.uber.org/zap"
)

// TestRunBackend_GracefulStop verifies the lifecycle: RunBackend binds and serves, and returns nil
// (no error) once its context is cancelled — the SIGINT/SIGTERM path. Port :0 lets the OS pick a free
// port so the test never collides with a real service.
func TestRunBackend_GracefulStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunBackend(ctx, config.BackendConfig{IngestAddr: "127.0.0.1:0", LogLevel: "error"}, zap.NewNop())
	}()

	time.Sleep(100 * time.Millisecond) // let it bind + start serving
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunBackend returned error on graceful stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunBackend did not stop within 2s of context cancel")
	}
}

// TestRunBackend_BindError surfaces a listen failure (a malformed address never binds). It also
// passes a nil logger to exercise the nil-logger guard (must not panic).
func TestRunBackend_BindError(t *testing.T) {
	err := RunBackend(context.Background(), config.BackendConfig{IngestAddr: "not-an-address:99999999", LogLevel: "error"}, nil)
	if err == nil {
		t.Fatal("want bind error for invalid address, got nil")
	}
}
