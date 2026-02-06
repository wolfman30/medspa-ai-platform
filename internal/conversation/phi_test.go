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

// Booking intent with "can i" should NOT trigger medical advice deflection
func TestRedactSensitive_AllowsCanIGetBotox(t *testing.T) {
	message := "Can I get Botox this week?"
	redacted, ok := RedactSensitive(message)
	if ok {
		t.Fatalf("expected booking message 'Can I get Botox' to pass through, but it was flagged")
	}
	if redacted != message {
		t.Fatalf("expected message to pass through unchanged, got %q", redacted)
	}
}

func TestRedactSensitive_AllowsShouldIBookFiller(t *testing.T) {
	message := "Should I get filler for my lips?"
	redacted, ok := RedactSensitive(message)
	if ok {
		t.Fatalf("expected booking message 'Should I get filler' to pass through, but it was flagged")
	}
	if redacted != message {
		t.Fatalf("expected message to pass through unchanged, got %q", redacted)
	}
}

func TestRedactSensitive_AllowsCanIScheduleLaser(t *testing.T) {
	message := "Can I come in for laser treatment tomorrow?"
	redacted, ok := RedactSensitive(message)
	if ok {
		t.Fatalf("expected booking message to pass through, but it was flagged")
	}
	if redacted != message {
		t.Fatalf("expected message to pass through unchanged, got %q", redacted)
	}
}

// Weak cue + medical-specific context SHOULD trigger
func TestRedactSensitive_DetectsCanIWithPregnancy(t *testing.T) {
	message := "Can I get Botox while pregnant?"
	redacted, ok := RedactSensitive(message)
	if !ok {
		t.Fatalf("expected medical advice about pregnancy to be detected")
	}
	if redacted != "[REDACTED]" {
		t.Fatalf("expected redacted placeholder, got %q", redacted)
	}
}

func TestRedactSensitive_DetectsShouldIWithMedication(t *testing.T) {
	message := "Should I stop taking my blood pressure medication before Botox?"
	redacted, ok := RedactSensitive(message)
	if !ok {
		t.Fatalf("expected medical advice about medication to be detected")
	}
	if redacted != "[REDACTED]" {
		t.Fatalf("expected redacted placeholder, got %q", redacted)
	}
}

// Strong cue + service context SHOULD still trigger
func TestRedactSensitive_DetectsSideEffectsOfBotox(t *testing.T) {
	message := "What are the side effects of Botox?"
	redacted, ok := RedactSensitive(message)
	if !ok {
		t.Fatalf("expected side effects question to be detected")
	}
	if redacted != "[REDACTED]" {
		t.Fatalf("expected redacted placeholder, got %q", redacted)
	}
}

func TestRedactSensitive_DetectsIsItSafeLaser(t *testing.T) {
	message := "Is it safe to get laser treatment?"
	redacted, ok := RedactSensitive(message)
	if !ok {
		t.Fatalf("expected safety question to be detected")
	}
	if redacted != "[REDACTED]" {
		t.Fatalf("expected redacted placeholder, got %q", redacted)
	}
}

func TestRedactSensitive_DetectsCanIMixIbuprofen(t *testing.T) {
	message := "Can I take ibuprofen after my filler appointment?"
	redacted, ok := RedactSensitive(message)
	if !ok {
		t.Fatalf("expected medication interaction question to be detected")
	}
	if redacted != "[REDACTED]" {
		t.Fatalf("expected redacted placeholder, got %q", redacted)
	}
}
