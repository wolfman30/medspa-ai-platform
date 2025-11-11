package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestNewLevels(t *testing.T) {
	tests := []struct {
		name   string
		level  string
		enable slog.Level
	}{
		{"debug level", "debug", slog.LevelDebug},
		{"warn level", "warn", slog.LevelWarn},
		{"default info", "", slog.LevelInfo},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := New(tt.level)
			if !logger.Enabled(ctx, tt.enable) {
				t.Fatalf("expected level %s to be enabled", tt.enable)
			}
		})
	}
}

func TestDefaultLogger(t *testing.T) {
	logger := Default()
	if logger == nil {
		t.Fatal("expected default logger")
	}
}
