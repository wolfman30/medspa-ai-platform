package events

import "time"

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
}

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

type BookingConfirmedV1 struct {
	EventID        string     `json:"event_id"`
	OrgID          string     `json:"org_id"`
	LeadID         string     `json:"lead_id"`
	BookingID      string     `json:"booking_id"`
	ConfirmedAt    time.Time  `json:"confirmed_at"`
	ScheduledFor   *time.Time `json:"scheduled_for,omitempty"`
	ConversationID string     `json:"conversation_id,omitempty"`
}
