package monitor

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a new slog.Logger with the given level string.
// Returns the logger and the LevelVar so the level can be updated at runtime.
func NewLogger(level string) (*slog.Logger, *slog.LevelVar) {
	levelVar := &slog.LevelVar{}
	levelVar.Set(ParseLogLevel(level))
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: levelVar}))
	return logger, levelVar
}

// ParseLogLevel converts a log level string to slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
