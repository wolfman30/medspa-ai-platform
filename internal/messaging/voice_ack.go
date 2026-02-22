package messaging

import (
	"fmt"
	"strings"
)

// InstantAckMessageWithCallback returns the missed-call ack with a callback option
// for clinics that have voice AI enabled.
func InstantAckMessageWithCallback(clinicName string) string {
	name := strings.TrimSpace(clinicName)
	if name == "" {
		return "Hi there! Sorry we missed your call. I'm the virtual receptionist and can help by text, or I can call you right back for a quick chat. Just reply \"call me back\" if you'd prefer a call! Otherwise, how can I help - booking an appointment or a quick question? Reply STOP to opt out."
	}
	return fmt.Sprintf("Hi there! Sorry we missed your call. I'm the virtual receptionist for %s and can help by text, or I can call you right back for a quick chat. Just reply \"call me back\" if you'd prefer a call! Otherwise, how can I help - booking an appointment or a quick question? Reply STOP to opt out.", name)
}
