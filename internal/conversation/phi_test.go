package conversation

import "testing"

func TestRedactSensitive_RedactsPHI(t *testing.T) {
	message := "I have diabetes and need advice"
	redacted, ok := RedactSensitive(message)
	if !ok {
		t.Fatalf("expected PHI to be detected")
	}
	if redacted != "[REDACTED]" {
		t.Fatalf("expected redacted placeholder, got %q", redacted)
	}
}

func TestRedactSensitive_RedactsMedicalAdvice(t *testing.T) {
	message := "Is it safe for me to take ibuprofen before Botox?"
	redacted, ok := RedactSensitive(message)
	if !ok {
		t.Fatalf("expected medical advice to be detected")
	}
	if redacted != "[REDACTED]" {
		t.Fatalf("expected redacted placeholder, got %q", redacted)
	}
}

func TestRedactSensitive_AllowsBookingMessage(t *testing.T) {
	message := "I want to book a facial on Friday afternoon"
	redacted, ok := RedactSensitive(message)
	if ok {
		t.Fatalf("expected booking message to pass through")
	}
	if redacted != message {
		t.Fatalf("expected message to pass through, got %q", redacted)
	}
}
