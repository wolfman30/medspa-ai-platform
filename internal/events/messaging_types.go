package events

import "time"

// MessageReceivedV1 captures inbound SMS/MMS metadata.
type MessageReceivedV1 struct {
	MessageID     string    `json:"message_id"`
	ClinicID      string    `json:"clinic_id"`
	FromE164      string    `json:"from_e164"`
	ToE164        string    `json:"to_e164"`
	Body          string    `json:"body"`
	MediaURLs     []string  `json:"media_urls,omitempty"`
	Provider      string    `json:"provider"`
	ReceivedAt    time.Time `json:"received_at"`
	TelnyxEventID string    `json:"telnyx_event_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func (MessageReceivedV1) EventType() string {
	return "messaging.message.received.v1"
}

// MessageSentV1 captures outbound messaging attempts.
type MessageSentV1 struct {
	MessageID            string    `json:"message_id"`
	ClinicID             string    `json:"clinic_id"`
	FromE164             string    `json:"from_e164"`
	ToE164               string    `json:"to_e164"`
	Body                 string    `json:"body"`
	MediaURLs            []string  `json:"media_urls,omitempty"`
	Provider             string    `json:"provider"`
	SentAt               time.Time `json:"sent_at"`
	QuietHoursSuppressed bool      `json:"quiet_hours_suppressed"`
	OptOutSuppressed     bool      `json:"opt_out_suppressed"`
	TemplateName         string    `json:"template_name,omitempty"`
	ProviderMessageID    string    `json:"provider_message_id,omitempty"`
}

func (MessageSentV1) EventType() string {
	return "messaging.message.sent.v1"
}

// HostedOrderActivatedV1 notifies when a hosted messaging order activates.
type HostedOrderActivatedV1 struct {
	OrderID         string    `json:"order_id"`
	ClinicID        string    `json:"clinic_id"`
	E164Number      string    `json:"e164_number"`
	ProviderOrderID string    `json:"provider_order_id,omitempty"`
	ActivatedAt     time.Time `json:"activated_at"`
}

func (HostedOrderActivatedV1) EventType() string {
	return "messaging.hosted_order.activated.v1"
}

// BrandCreatedV1 signals when a 10DLC brand has been registered.
type BrandCreatedV1 struct {
	BrandInternalID string    `json:"brand_internal_id"`
	ClinicID        string    `json:"clinic_id"`
	BrandID         string    `json:"brand_id"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

func (BrandCreatedV1) EventType() string {
	return "messaging.ten_dlc.brand.created.v1"
}

// CampaignApprovedV1 captures approval of a 10DLC campaign.
type CampaignApprovedV1 struct {
	CampaignInternalID string    `json:"campaign_internal_id"`
	BrandInternalID    string    `json:"brand_internal_id"`
	CampaignID         string    `json:"campaign_id"`
	UseCase            string    `json:"use_case"`
	SampleMessages     []string  `json:"sample_messages,omitempty"`
	ApprovedAt         time.Time `json:"approved_at"`
}

func (CampaignApprovedV1) EventType() string {
	return "messaging.ten_dlc.campaign.approved.v1"
}
