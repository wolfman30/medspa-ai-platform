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

const firstAckMedicalNote = "I can't provide medical advice over text."

// SmsAckMessageFirst includes the medical advice note on the very first ack.
const SmsAckMessageFirst = SmsAckMessageFirstBase + " " + firstAckMedicalNote

// smsAckMessagesFirst are varied acks for the first inbound message.
var smsAckMessagesFirst = buildFirstAckMessages([]string{
	SmsAckMessageFirstBase,
	"Thanks for reaching out - one moment while I check.",
	"Thanks! Give me a second to look that up.",
	"Got it! Let me check on that.",
})

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

func buildFirstAckMessages(base []string) []string {
	messages := make([]string, 0, len(base))
	for _, msg := range base {
		messages = append(messages, addMedicalNote(msg))
	}
	return messages
}

func addMedicalNote(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return trimmed
	}
	if strings.Contains(strings.ToLower(trimmed), "medical advice") {
		return trimmed
	}
	return trimmed + " " + firstAckMedicalNote
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
