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

	// Test 1: Verify the logger is functional by actually using it
	// (Won't panic if properly initialized)
	logger.Info("test message", "key", "value")

	// Test 2: Verify the default level is "info"
	ctx := context.Background()
	if !logger.Enabled(ctx, slog.LevelInfo) {
		t.Error("Default() should enable info level")
	}
	if logger.Enabled(ctx, slog.LevelDebug) {
		t.Error("Default() should not enable debug level (info is higher)")
	}

	// Test 3: Verify the underlying slog.Logger is properly initialized
	if logger.Logger == nil {
		t.Fatal("Default() returned Logger with nil slog.Logger (should be impossible)")
	}

	// Test 4: Verify Default() returns a new instance each time (not a singleton)
	logger2 := Default()
	if logger == logger2 {
		t.Error("Default() returned the same instance twice - expected new instances")
	}
}
