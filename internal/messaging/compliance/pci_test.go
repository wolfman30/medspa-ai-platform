package compliance

import (
	"testing"
)

func TestRedactPAN(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantRedact  bool
		wantContain string
	}{
		{"empty", "", false, ""},
		{"no card", "Hello world", false, ""},
		{"visa card", "My card is 4111111111111111", true, "[REDACTED_CARD_1111]"},
		{"card with spaces", "4111 1111 1111 1111", true, "[REDACTED_CARD_1111]"},
		{"card with dashes", "4111-1111-1111-1111", true, "[REDACTED_CARD_1111]"},
		{"text around card", "pay with 4111111111111111 please", true, "[REDACTED_CARD_1111]"},
		{"invalid luhn", "1234567890123456", false, ""},
		{"too short", "411111111111", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, redacted := RedactPAN(tt.input)
			if redacted != tt.wantRedact {
				t.Errorf("RedactPAN(%q) redacted = %v, want %v", tt.input, redacted, tt.wantRedact)
			}
			if tt.wantContain != "" && redacted {
				if !contains(got, tt.wantContain) {
					t.Errorf("RedactPAN(%q) = %q, want to contain %q", tt.input, got, tt.wantContain)
				}
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestIsStart(t *testing.T) {
	d := NewDetector()
	tests := []struct {
		input string
		want  bool
	}{
		{"start", true},
		{"START", true},
		{"subscribe", true},
		{"unstop", true},
		{"please start", true},
		{"hello", false},
		{"stop", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := d.IsStart(tt.input); got != tt.want {
				t.Errorf("IsStart(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
