// Package logging creates a *slog.Logger whose level can be adjusted at
// runtime via a *slog.LevelVar.
package logging

import (
	"log/slog"
	"os"
)

// New returns a logger that writes to stderr at whatever level levelVar
// currently holds. The caller retains the LevelVar so the level can be
// changed after flag parsing (e.g. --verbose).
func New(levelVar *slog.LevelVar) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: levelVar}))
}
