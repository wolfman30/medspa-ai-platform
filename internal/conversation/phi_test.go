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

func TestRedactSensitive_AllowsExistingPatient(t *testing.T) {
	// "existing" should NOT trigger PHI detection even though it contains "sti"
	message := "I'm an existing patient"
	redacted, ok := RedactSensitive(message)
	if ok {
		t.Fatalf("expected 'existing patient' message to pass through, but it was flagged")
	}
	if redacted != message {
		t.Fatalf("expected message to pass through unchanged, got %q", redacted)
	}
}

func TestRedactSensitive_DetectsActualSTI(t *testing.T) {
	// Actual "sti" as a standalone word should still trigger PHI detection
	message := "I have an sti and want to know if I can get botox"
	redacted, ok := RedactSensitive(message)
	if !ok {
		t.Fatalf("expected actual STI mention to be detected")
	}
	if redacted != "[REDACTED]" {
		t.Fatalf("expected redacted placeholder, got %q", redacted)
	}
}
