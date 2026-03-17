// Package logging provides a global *slog.Logger controlled by the
// CODEBOX_LOGGING environment variable.
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// nopHandler discards all log records.
type nopHandler struct{}

func (nopHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (nopHandler) Handle(context.Context, slog.Record) error { return nil }
func (nopHandler) WithAttrs([]slog.Attr) slog.Handler        { return nopHandler{} }
func (nopHandler) WithGroup(string) slog.Handler             { return nopHandler{} }

var logger *slog.Logger

func init() {
	initLogger()
}

func initLogger() {
	raw := os.Getenv("CODEBOX_LOGGING")
	if raw == "" {
		logger = slog.New(nopHandler{})
		return
	}

	var level slog.Level
	switch strings.ToLower(raw) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// Get returns the configured logger.
func Get() *slog.Logger {
	return logger
}

// Init re-reads CODEBOX_LOGGING and reconfigures the global logger.
// Exported for testing; not intended for production use.
func Init() {
	initLogger()
}
