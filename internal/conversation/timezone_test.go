package conversation

import (
	"testing"
	"time"
)

func TestParseSlotTime(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		tz       string
		wantHour int
		wantMin  int
		wantErr  bool
	}{
		{
			name:     "RFC3339 with offset -05:00",
			raw:      "2026-02-24T19:00:00-05:00",
			tz:       "America/New_York",
			wantHour: 19,
			wantMin:  0,
		},
		{
			name:     "RFC3339 UTC — converts to ET",
			raw:      "2026-02-25T00:00:00Z",
			tz:       "America/New_York",
			wantHour: 19, // midnight UTC = 7 PM ET (EST)
			wantMin:  0,
		},
		{
			name:     "naive datetime — treated as clinic local",
			raw:      "2026-02-24T19:00:00",
			tz:       "America/New_York",
			wantHour: 19,
			wantMin:  0,
		},
		{
			name:     "naive datetime — different timezone",
			raw:      "2026-02-24T14:30:00",
			tz:       "America/Chicago",
			wantHour: 14,
			wantMin:  30,
		},
		{
			name:    "garbage input",
			raw:     "not-a-date",
			tz:      "America/New_York",
			wantErr: true,
		},
		{
			name:     "invalid timezone falls back to UTC",
			raw:      "2026-02-24T19:00:00Z",
			tz:       "Invalid/Zone",
			wantHour: 19,
			wantMin:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSlotTime(tt.raw, tt.tz)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Hour() != tt.wantHour || got.Minute() != tt.wantMin {
				t.Errorf("got %d:%02d, want %d:%02d (full: %s)", got.Hour(), got.Minute(), tt.wantHour, tt.wantMin, got)
			}
		})
	}
}

func TestClinicLocation(t *testing.T) {
	loc := ClinicLocation("America/New_York")
	if loc.String() != "America/New_York" {
		t.Errorf("got %s, want America/New_York", loc)
	}

	loc = ClinicLocation("")
	if loc != time.UTC {
		t.Errorf("empty timezone should return UTC, got %s", loc)
	}

	loc = ClinicLocation("Invalid/Zone")
	if loc != time.UTC {
		t.Errorf("invalid timezone should return UTC, got %s", loc)
	}
}

func TestFormatAppointmentConfirmation(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	apptTime := time.Date(2026, 2, 24, 19, 0, 0, 0, loc)

	msg := FormatAppointmentConfirmation("Lip Filler", apptTime, "Forever 22 Med Spa")

	if msg == "" {
		t.Fatal("confirmation message should not be empty")
	}

	// Should contain key info
	for _, want := range []string{"Lip Filler", "7:00 PM", "February 24", "Forever 22", "24-hour cancellation"} {
		if !containsStr(msg, want) {
			t.Errorf("confirmation missing %q:\n%s", want, msg)
		}
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
