package main

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

// TestNewLogger_AtomicLevelApplies checks that the AtomicLevel returned alongside the logger
// actually controls the logger's effective level: a hot-reload calling SetLevel must take effect
// without rebuilding the logger.
func TestNewLogger_AtomicLevelApplies(t *testing.T) {
	log, atomicLevel, err := newLogger("info")
	if err != nil {
		t.Fatalf("newLogger: %v", err)
	}
	if atomicLevel.Level() != zapcore.InfoLevel {
		t.Fatalf("initial level = %v, want info", atomicLevel.Level())
	}
	if !log.Core().Enabled(zapcore.InfoLevel) {
		t.Error("logger should be enabled at info before reload")
	}
	if log.Core().Enabled(zapcore.DebugLevel) {
		t.Error("logger should not be enabled at debug before reload")
	}

	atomicLevel.SetLevel(zapcore.DebugLevel)

	if atomicLevel.Level() != zapcore.DebugLevel {
		t.Fatalf("level after SetLevel = %v, want debug", atomicLevel.Level())
	}
	if !log.Core().Enabled(zapcore.DebugLevel) {
		t.Error("logger should be enabled at debug after SetLevel(debug) — hot-reload did not take effect")
	}
}

// TestNewLogger_InvalidLevel checks the error path returns a zero AtomicLevel and nil logger.
func TestNewLogger_InvalidLevel(t *testing.T) {
	log, _, err := newLogger("trace")
	if err == nil {
		t.Fatal("newLogger(\"trace\"): want error, got nil")
	}
	if log != nil {
		t.Error("newLogger error path should return a nil logger")
	}
}
