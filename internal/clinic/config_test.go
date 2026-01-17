package clinic

import (
	"testing"
	"time"
)

func TestIsOpenAt(t *testing.T) {
	cfg := DefaultConfig("test-org")

	// Use the clinic's timezone
	loc, _ := time.LoadLocation("America/New_York")

	// Monday 10 AM EST - should be open
	monday10am := time.Date(2025, 12, 8, 10, 0, 0, 0, loc) // Monday
	if !cfg.IsOpenAt(monday10am) {
		t.Error("expected clinic to be open Monday 10 AM")
	}

	// Saturday 10 AM EST - should be closed
	saturday := time.Date(2025, 12, 13, 10, 0, 0, 0, loc)
	if cfg.IsOpenAt(saturday) {
		t.Error("expected clinic to be closed Saturday")
	}

	// Monday 7 AM EST - before opening
	monday7am := time.Date(2025, 12, 8, 7, 0, 0, 0, loc)
	if cfg.IsOpenAt(monday7am) {
		t.Error("expected clinic to be closed at 7 AM")
	}
}

func TestNextOpenTime(t *testing.T) {
	cfg := DefaultConfig("test-org")

	// Friday 8 PM EST - should return Monday 9 AM
	loc, _ := time.LoadLocation("America/New_York")
	friday8pm := time.Date(2025, 12, 5, 20, 0, 0, 0, loc)

	next := cfg.NextOpenTime(friday8pm)
	if next.Weekday() != time.Monday {
		t.Errorf("expected next open to be Monday, got %s", next.Weekday())
	}
	if next.Hour() != 9 {
		t.Errorf("expected next open at 9 AM, got %d", next.Hour())
	}
}

func TestBusinessHoursContext(t *testing.T) {
	cfg := DefaultConfig("test-org")
	cfg.Name = "Glow MedSpa"

	loc, _ := time.LoadLocation("America/New_York")
	friday8pm := time.Date(2025, 12, 5, 20, 0, 0, 0, loc)

	ctx := cfg.BusinessHoursContext(friday8pm)

	if ctx == "" {
		t.Error("expected non-empty context")
	}

	// Should mention the clinic is closed
	if !contains(ctx, "CLOSED") {
		t.Errorf("expected context to mention CLOSED, got: %s", ctx)
	}

	// Should mention next open time
	if !contains(ctx, "Monday") {
		t.Errorf("expected context to mention Monday, got: %s", ctx)
	}

	// Should include callback instruction
	if !contains(ctx, "CALLBACK INSTRUCTION") {
		t.Errorf("expected context to include callback instruction, got: %s", ctx)
	}
}

func TestExpectedCallbackTime(t *testing.T) {
	cfg := DefaultConfig("test-org")
	loc, _ := time.LoadLocation("America/New_York")

	tests := []struct {
		name     string
		time     time.Time
		contains string
	}{
		{
			name:     "Friday 6pm - should say Monday",
			time:     time.Date(2025, 12, 5, 18, 0, 0, 0, loc),
			contains: "Monday",
		},
		{
			name:     "Saturday - should say Monday",
			time:     time.Date(2025, 12, 6, 14, 0, 0, 0, loc),
			contains: "Monday",
		},
		{
			name:     "Monday 7am before open - should say today or tomorrow",
			time:     time.Date(2025, 12, 8, 7, 0, 0, 0, loc),
			contains: "9 AM",
		},
		{
			name:     "Wednesday 8pm after close - should say tomorrow",
			time:     time.Date(2025, 12, 10, 20, 0, 0, 0, loc),
			contains: "tomorrow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.ExpectedCallbackTime(tt.time)
			if !contains(result, tt.contains) {
				t.Errorf("ExpectedCallbackTime(%v) = %q, want to contain %q", tt.time, result, tt.contains)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
