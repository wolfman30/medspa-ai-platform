// Package events defines domain event types and infrastructure for reliable
// event delivery via the transactional outbox pattern.
package events

import "time"

// ConversationMessageReceivedV1 is emitted when an inbound SMS or channel
// message is received and associated with a conversation.
type ConversationMessageReceivedV1 struct {
	EventID        string    `json:"event_id"`
	OrgID          string    `json:"org_id"`
	ConversationID string    `json:"conversation_id"`
	LeadID         string    `json:"lead_id"`
	Channel        string    `json:"channel"`
	Body           string    `json:"body"`
	ReceivedAt     time.Time `json:"received_at"`
	FromNumber     string    `json:"from_number,omitempty"`
	ToNumber       string    `json:"to_number,omitempty"`
}

// DepositRequestedV1 is emitted when a deposit checkout link is generated
// and sent to a patient for a booking intent.
type DepositRequestedV1 struct {
	EventID         string    `json:"event_id"`
	OrgID           string    `json:"org_id"`
	LeadID          string    `json:"lead_id"`
	AmountCents     int64     `json:"amount_cents"`
	BookingIntentID string    `json:"booking_intent_id"`
	RequestedAt     time.Time `json:"requested_at"`
	CheckoutURL     string    `json:"checkout_url"`
	Provider        string    `json:"provider"`
}

// PaymentSucceededV1 is emitted when a deposit payment is successfully
// captured by a payment provider (Square or Stripe).
type PaymentSucceededV1 struct {
	EventID         string     `json:"event_id"`
	OrgID           string     `json:"org_id"`
	LeadID          string     `json:"lead_id"`
	BookingIntentID string     `json:"booking_intent_id,omitempty"`
	Provider        string     `json:"provider"`
	ProviderRef     string     `json:"provider_ref"`
	AmountCents     int64      `json:"amount_cents"`
	OccurredAt      time.Time  `json:"occurred_at"`
	LeadPhone       string     `json:"lead_phone,omitempty"`
	LeadName        string     `json:"lead_name,omitempty"`
	FromNumber      string     `json:"from_number,omitempty"`
	ScheduledFor    *time.Time `json:"scheduled_for,omitempty"`
	ServiceName     string     `json:"service_name,omitempty"`
}

// PaymentFailedV1 is emitted when a deposit payment attempt fails or is
// declined by the payment provider.
type PaymentFailedV1 struct {
	EventID         string    `json:"event_id"`
	OrgID           string    `json:"org_id"`
	LeadID          string    `json:"lead_id"`
	BookingIntentID string    `json:"booking_intent_id,omitempty"`
	Provider        string    `json:"provider"`
	ProviderRef     string    `json:"provider_ref"`
	AmountCents     int64     `json:"amount_cents"`
	OccurredAt      time.Time `json:"occurred_at"`
	LeadPhone       string    `json:"lead_phone,omitempty"`
	FromNumber      string    `json:"from_number,omitempty"`
	FailureStatus   string    `json:"failure_status,omitempty"`
}

// BookingConfirmedV1 is emitted when an appointment is successfully booked
// on the EMR (e.g. Moxie) after deposit payment.
type BookingConfirmedV1 struct {
	EventID        string     `json:"event_id"`
	OrgID          string     `json:"org_id"`
	LeadID         string     `json:"lead_id"`
	BookingID      string     `json:"booking_id"`
	ConfirmedAt    time.Time  `json:"confirmed_at"`
	ScheduledFor   *time.Time `json:"scheduled_for,omitempty"`
	ConversationID string     `json:"conversation_id,omitempty"`
}
