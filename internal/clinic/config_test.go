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

func TestUsesMoxieBooking(t *testing.T) {
	tests := []struct {
		name            string
		bookingPlatform string
		wantMoxie       bool
		wantSquare      bool
	}{
		{
			name:            "empty platform defaults to Square",
			bookingPlatform: "",
			wantMoxie:       false,
			wantSquare:      true,
		},
		{
			name:            "moxie platform",
			bookingPlatform: "moxie",
			wantMoxie:       true,
			wantSquare:      false,
		},
		{
			name:            "Moxie platform (uppercase)",
			bookingPlatform: "Moxie",
			wantMoxie:       true,
			wantSquare:      false,
		},
		{
			name:            "MOXIE platform (all caps)",
			bookingPlatform: "MOXIE",
			wantMoxie:       true,
			wantSquare:      false,
		},
		{
			name:            "square platform",
			bookingPlatform: "square",
			wantMoxie:       false,
			wantSquare:      true,
		},
		{
			name:            "Square platform (capitalized)",
			bookingPlatform: "Square",
			wantMoxie:       false,
			wantSquare:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig("test-org")
			cfg.BookingPlatform = tt.bookingPlatform

			if got := cfg.UsesMoxieBooking(); got != tt.wantMoxie {
				t.Errorf("UsesMoxieBooking() = %v, want %v", got, tt.wantMoxie)
			}
			if got := cfg.UsesSquarePayment(); got != tt.wantSquare {
				t.Errorf("UsesSquarePayment() = %v, want %v", got, tt.wantSquare)
			}
		})
	}
}

func TestUsesMoxieBooking_NilConfig(t *testing.T) {
	var cfg *Config = nil

	if cfg.UsesMoxieBooking() {
		t.Error("expected nil config to return false for UsesMoxieBooking()")
	}
	if !cfg.UsesSquarePayment() {
		t.Error("expected nil config to return true for UsesSquarePayment()")
	}
}

func TestResolveServiceName(t *testing.T) {
	cfg := &Config{
		ServiceAliases: map[string]string{
			"botox":            "Tox",
			"wrinkle relaxers": "Tox",
			"jeuveau":          "Tox",
		},
	}

	// Alias hit
	if got := cfg.ResolveServiceName("Botox"); got != "Tox" {
		t.Errorf("expected Tox, got %s", got)
	}
	// Case-insensitive
	if got := cfg.ResolveServiceName("BOTOX"); got != "Tox" {
		t.Errorf("expected Tox, got %s", got)
	}
	// No alias → passthrough
	if got := cfg.ResolveServiceName("filler"); got != "filler" {
		t.Errorf("expected filler, got %s", got)
	}
	// Nil config
	var nilCfg *Config
	if got := nilCfg.ResolveServiceName("Botox"); got != "Botox" {
		t.Errorf("expected Botox, got %s", got)
	}
	// Empty aliases
	cfg2 := &Config{}
	if got := cfg2.ResolveServiceName("Botox"); got != "Botox" {
		t.Errorf("expected Botox, got %s", got)
	}
}

func TestResolveProviderID(t *testing.T) {
	cfg := &Config{
		MoxieConfig: &MoxieConfig{
			ProviderNames: map[string]string{
				"33950": "Brandi Sesock",
				"38627": "Gale Tesar",
			},
		},
	}

	tests := []struct {
		input string
		want  string
	}{
		{"Gale Tesar", "38627"},
		{"gale tesar", "38627"},
		{"Gale", "38627"},
		{"gale", "38627"},
		{"Brandi Sesock", "33950"},
		{"Brandi", "33950"},
		{"brandi", "33950"},
		{"no preference", ""},
		{"", ""},
		{"Unknown Provider", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cfg.ResolveProviderID(tt.input)
			if got != tt.want {
				t.Errorf("ResolveProviderID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestUsesManualHandoff(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected bool
	}{
		{"nil config", nil, true},
		{"empty config", &Config{}, true},
		{"explicit manual", &Config{BookingAdapter: "manual"}, true},
		{"moxie adapter", &Config{BookingAdapter: "moxie"}, false},
		{"boulevard adapter", &Config{BookingAdapter: "boulevard"}, false},
		{"moxie platform no adapter", &Config{BookingPlatform: "moxie"}, false},
		{"square platform no adapter", &Config{BookingPlatform: "square"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.UsesManualHandoff(); got != tt.expected {
				t.Errorf("UsesManualHandoff() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestResolvedBookingAdapter(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected string
	}{
		{"nil config", nil, "manual"},
		{"empty config", &Config{}, "manual"},
		{"explicit manual", &Config{BookingAdapter: "manual"}, "manual"},
		{"explicit moxie", &Config{BookingAdapter: "moxie"}, "moxie"},
		{"moxie platform", &Config{BookingPlatform: "moxie"}, "moxie"},
		{"boulevard", &Config{BookingAdapter: "boulevard"}, "boulevard"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolvedBookingAdapter(); got != tt.expected {
				t.Errorf("ResolvedBookingAdapter() = %q, want %q", got, tt.expected)
			}
		})
	}
}
