package messaging

// InstantAckMessage is the fast auto-reply sent immediately for missed-call text-backs.
const InstantAckMessage = "Hi there! Sorry we missed your call. I'm the virtual receptionist and can answer questions or book an appointment. Which service are you interested in? Reply STOP to opt out."

// SmsAckMessage is a neutral quick ack for inbound SMS while the AI prepares a full reply.
const SmsAckMessage = "Got it - give me a moment to help you."
