package monitor

import (
	"log/slog"
	"testing"
)

func TestNewLogger(t *testing.T) {
	logger, levelVar := NewLogger("debug")

	if logger == nil {
		t.Fatal("Logger should not be nil")
	}
	if levelVar == nil {
		t.Fatal("LevelVar should not be nil")
	}
	if levelVar.Level() != slog.LevelDebug {
		t.Errorf("Level = %v, want %v", levelVar.Level(), slog.LevelDebug)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tt := range tests {
		got := ParseLogLevel(tt.input)
		if got != tt.want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNewLoggerLevelUpdate(t *testing.T) {
	_, levelVar := NewLogger("info")

	if levelVar.Level() != slog.LevelInfo {
		t.Fatalf("Initial level should be info")
	}

	levelVar.Set(slog.LevelDebug)
	if levelVar.Level() != slog.LevelDebug {
		t.Errorf("Level should be debug after update, got %v", levelVar.Level())
	}
}
