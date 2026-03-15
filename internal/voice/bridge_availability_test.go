package voice

import "testing"

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
