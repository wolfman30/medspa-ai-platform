package voice

import (
	"testing"
	"time"
)

func TestServiceConfirmationPattern(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		matched bool
	}{
		{"Great, Botox! What's your full name?", "Botox", true},
		{"Perfect, microneedling! What's your full name?", "microneedling", true},
		{"Awesome, HydraFacial! What's your name?", "HydraFacial", true},
		{"Sure, lip filler! What's your full name?", "lip filler", true},
		{"Great, Botox/Dysport! What's your name?", "Botox/Dysport", true},
		{"What service are you interested in?", "", false},
		{"I heard your name as Andrew Wolf", "", false},
		{"[user] I want botox", "", false},
	}
	for _, tt := range tests {
		matches := serviceConfirmationPattern.FindStringSubmatch(tt.input)
		if tt.matched {
			if len(matches) < 2 {
				t.Errorf("expected match for %q, got none", tt.input)
				continue
			}
			got := matches[1]
			if got != tt.want {
				t.Errorf("for %q: got %q, want %q", tt.input, got, tt.want)
			}
		} else {
			if len(matches) >= 2 && matches[1] != "" {
				t.Errorf("expected no match for %q, got %q", tt.input, matches[1])
			}
		}
	}
}

func TestParseVoiceTimePreferences(t *testing.T) {
	tests := []struct {
		input     string
		wantAfter int
		wantDays  int
	}{
		{"weekdays after four pm", 16, 5},
		{"monday after 4", 16, 1},
		{"tuesday wednesday after 3 pm", 15, 2},
		{"morning", -1, 0},
		{"afternoon", 12, 0},
		{"evening", 17, 0},
	}
	for _, tt := range tests {
		prefs := parseVoiceTimePreferences(tt.input)
		if prefs.afterHour != tt.wantAfter {
			t.Errorf("%q: afterHour = %d, want %d", tt.input, prefs.afterHour, tt.wantAfter)
		}
		if len(prefs.daysOfWeek) != tt.wantDays {
			t.Errorf("%q: days = %d, want %d", tt.input, len(prefs.daysOfWeek), tt.wantDays)
		}
	}
}

func TestMatchesVoicePreferences(t *testing.T) {
	// Tuesday March 31 at 4:45 PM
	slot445pm := time.Date(2026, 3, 31, 16, 45, 0, 0, time.UTC)
	// Monday March 16 at 11:00 AM
	slot11am := time.Date(2026, 3, 16, 11, 0, 0, 0, time.UTC)

	prefs := parseVoiceTimePreferences("weekdays after four pm")

	if !matchesVoicePreferences(slot445pm, prefs) {
		t.Error("4:45 PM on Tuesday should match 'weekdays after 4pm'")
	}
	if matchesVoicePreferences(slot11am, prefs) {
		t.Error("11:00 AM on Monday should NOT match 'weekdays after 4pm'")
	}
}
