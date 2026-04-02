package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestNew_DefaultLevel(t *testing.T) {
	var levelVar slog.LevelVar
	levelVar.Set(slog.LevelInfo)

	log := New(&levelVar)
	if log == nil {
		t.Fatal("New() returned nil logger")
	}

	if log.Handler().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("INFO level should not enable debug")
	}

	if !log.Handler().Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("INFO level should enable info")
	}
}

func TestNew_DebugLevel(t *testing.T) {
	var levelVar slog.LevelVar
	levelVar.Set(slog.LevelDebug)

	log := New(&levelVar)
	if log == nil {
		t.Fatal("New() returned nil logger")
	}

	if !log.Handler().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("DEBUG level should enable debug")
	}

	if !log.Handler().Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("DEBUG level should enable info")
	}
}

func TestNew_LevelVarChange(t *testing.T) {
	var levelVar slog.LevelVar
	levelVar.Set(slog.LevelInfo)

	log := New(&levelVar)

	// Debug should be disabled at INFO.
	if log.Handler().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("INFO level should not enable debug")
	}

	// Switch to DEBUG at runtime.
	levelVar.Set(slog.LevelDebug)

	if !log.Handler().Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("after switching to DEBUG, debug should be enabled")
	}
}
