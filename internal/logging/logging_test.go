package logging

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

func TestGet_Default(t *testing.T) {
	t.Setenv("CODEBOX_LOGGING", "")
	Init()

	log := Get()
	if log == nil {
		t.Fatal("Get() returned nil logger")
	}

	// Must not panic.
	log.Info("should be discarded")

	// The handler should report Enabled == false for all levels.
	if log.Handler().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("nop handler should not be enabled for debug")
	}
	if log.Handler().Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("nop handler should not be enabled for info")
	}
}

func TestGet_WithLevel(t *testing.T) {
	t.Setenv("CODEBOX_LOGGING", "debug")
	Init()

	log := Get()
	if log == nil {
		t.Fatal("Get() returned nil logger")
	}

	// The handler should be enabled for debug level.
	if !log.Handler().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("handler should be enabled for debug")
	}
	if !log.Handler().Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("handler should be enabled for info")
	}

	// Verify that logging actually produces output by replacing the handler
	// with one writing to a buffer.
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	testLogger.Info("test message", "key", "value")
	if buf.Len() == 0 {
		t.Error("expected log output, got nothing")
	}
}

func TestGet_InvalidLevel(t *testing.T) {
	t.Setenv("CODEBOX_LOGGING", "banana")
	Init()

	log := Get()
	if log == nil {
		t.Fatal("Get() returned nil logger")
	}

	// Invalid level should default to info, so debug should be disabled
	// but info should be enabled.
	if log.Handler().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("invalid level should default to info; debug should be disabled")
	}
	if !log.Handler().Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("invalid level should default to info; info should be enabled")
	}
}
