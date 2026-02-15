package messaging

import (
	"fmt"
	"math/rand"
	"strings"
)

// InstantAckMessage is the fast auto-reply sent immediately for missed-call text-backs.
const InstantAckMessage = "Hi there! Sorry we missed your call. I'm the virtual receptionist and can help by text—though I can't provide medical advice. How can I help today - booking an appointment or a quick question? Reply STOP to opt out."

// InstantAckMessageForClinic personalizes the missed-call ack with a clinic name when available.
func InstantAckMessageForClinic(clinicName string) string {
	name := strings.TrimSpace(clinicName)
	if name == "" {
		return InstantAckMessage
	}
	return fmt.Sprintf("Hi there! Sorry we missed your call. I'm the virtual receptionist for %s and can help by text—though I can't provide medical advice. How can I help today - booking an appointment or a quick question? Reply STOP to opt out.", name)
}

// PCIGuardrailMessage is sent when inbound SMS appears to contain payment card details.
const PCIGuardrailMessage = "For your security, please do not send credit card details by text. We can only take payments through our secure checkout link. If you'd like a deposit link, reply \"deposit\" and we'll send it. Reply STOP to opt out."

// SmsAckMessageFirst is the ack for the first inbound SMS in a conversation.
const SmsAckMessageFirstBase = "Got it - give me a moment to help you."

// SmsAckMessageFirst is kept for backward compatibility (e.g. IsSmsAckMessage).
const SmsAckMessageFirst = SmsAckMessageFirstBase

// smsAckMessagesFirst are varied acks for the first inbound message.
// NOTE: Medical advice disclaimer removed — it was off-putting on booking
// requests like "I want Botox." The missed-call ack (InstantAckMessage)
// still includes it naturally. The LLM handles medical deflection when needed.
var smsAckMessagesFirst = []string{
	SmsAckMessageFirstBase,
	"Thanks for reaching out - one moment while I check.",
	"Thanks! Give me a second to look that up.",
	"Got it! Let me check on that.",
}

// smsAckMessagesFollowUp are varied acks for follow-up messages to feel more human.
var smsAckMessagesFollowUp = []string{
	"Thanks - one moment...",
	"Got it. One sec.",
	"On it - just a moment.",
	"Checking now...",
	"Give me a second...",
}

// GetSmsAckMessage returns the appropriate ack message.
// isFirstMessage should be true for the first message in a conversation.
func GetSmsAckMessage(isFirstMessage bool) string {
	if isFirstMessage {
		return smsAckMessagesFirst[rand.Intn(len(smsAckMessagesFirst))]
	}
	return smsAckMessagesFollowUp[rand.Intn(len(smsAckMessagesFollowUp))]
}

// IsSmsAckMessage reports whether a message matches any configured ack response.
func IsSmsAckMessage(message string) bool {
	if message == "" {
		return false
	}
	if message == SmsAckMessageFirst {
		return true
	}
	for _, candidate := range smsAckMessagesFirst {
		if message == candidate {
			return true
		}
	}
	for _, candidate := range smsAckMessagesFollowUp {
		if message == candidate {
			return true
		}
	}
	return false
}
