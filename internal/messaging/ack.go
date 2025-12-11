package messaging

import (
	"math/rand"
)

// InstantAckMessage is the fast auto-reply sent immediately for missed-call text-backs.
const InstantAckMessage = "Hi there! Sorry we missed your call. I'm the virtual receptionist and can answer questions or book an appointment. Which service are you interested in? Reply STOP to opt out."

// SmsAckMessageFirst is the ack for the first inbound SMS in a conversation.
const SmsAckMessageFirst = "Got it - give me a moment to help you."

// smsAckMessagesFollowUp are varied acks for follow-up messages to feel more human.
var smsAckMessagesFollowUp = []string{
	"Ok, one moment please...",
	"Got it. Just a moment...",
	"One sec...",
	"Let me check on that...",
	"Sure, just a moment...",
}

// GetSmsAckMessage returns the appropriate ack message.
// isFirstMessage should be true for the first message in a conversation.
func GetSmsAckMessage(isFirstMessage bool) string {
	if isFirstMessage {
		return SmsAckMessageFirst
	}
	return smsAckMessagesFollowUp[rand.Intn(len(smsAckMessagesFollowUp))]
}
