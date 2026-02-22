package messaging

import (
	"strings"
	"testing"
)

func TestInstantAckMessageWithCallback_NoName(t *testing.T) {
	msg := InstantAckMessageWithCallback("")
	if !strings.Contains(msg, "call me back") {
		t.Error("expected callback option in message")
	}
	if !strings.Contains(msg, "Sorry we missed your call") {
		t.Error("expected missed call acknowledgment")
	}
	if !strings.Contains(msg, "STOP") {
		t.Error("expected opt-out notice")
	}
}

func TestInstantAckMessageWithCallback_WithName(t *testing.T) {
	msg := InstantAckMessageWithCallback("Forever 22 Med Spa")
	if !strings.Contains(msg, "Forever 22 Med Spa") {
		t.Error("expected clinic name in message")
	}
	if !strings.Contains(msg, "call me back") {
		t.Error("expected callback option")
	}
}

func TestInstantAckMessageWithCallback_DiffersFromStandard(t *testing.T) {
	standard := InstantAckMessageForClinic("Test Clinic")
	withCallback := InstantAckMessageWithCallback("Test Clinic")

	if standard == withCallback {
		t.Error("callback message should differ from standard message")
	}
	if !strings.Contains(withCallback, "call") {
		t.Error("callback message should mention calling")
	}
}
