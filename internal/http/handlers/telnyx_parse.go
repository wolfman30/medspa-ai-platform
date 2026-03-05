package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// telnyxEvent represents a normalized Telnyx webhook event.
type telnyxEvent struct {
	ID         string          `json:"id"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

// parseTelnyxEvent decodes a raw webhook body into a telnyxEvent,
// handling both the event-driven format (with data wrapper) and the
// direct message record format.
func parseTelnyxEvent(body []byte) (telnyxEvent, error) {
	// Try event-driven format first (with data wrapper)
	var wrapper struct {
		Data struct {
			ID         string          `json:"id"`
			EventType  string          `json:"event_type"`
			OccurredAt time.Time       `json:"occurred_at"`
			Payload    json.RawMessage `json:"payload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Data.ID != "" {
		return telnyxEvent{
			ID:         wrapper.Data.ID,
			EventType:  wrapper.Data.EventType,
			OccurredAt: wrapper.Data.OccurredAt,
			Payload:    wrapper.Data.Payload,
		}, nil
	}

	// Try message record format (no wrapper)
	var record struct {
		ID         string    `json:"id"`
		RecordType string    `json:"record_type"`
		ReceivedAt time.Time `json:"received_at"`
		Direction  string    `json:"direction"`
	}
	if err := json.Unmarshal(body, &record); err != nil {
		return telnyxEvent{}, err
	}

	// Convert to event format
	eventType := ""
	if record.RecordType == "message" && record.Direction == "inbound" {
		eventType = "message.received"
	} else if record.RecordType == "message" && record.Direction == "outbound" {
		eventType = "message.delivery_status"
	}

	return telnyxEvent{
		ID:         record.ID,
		EventType:  eventType,
		OccurredAt: record.ReceivedAt,
		Payload:    body, // Use the whole body as payload
	}, nil
}

// telnyxMessagePayload represents the payload of an inbound Telnyx message.
type telnyxMessagePayload struct {
	ID        string   `json:"id"`
	Direction string   `json:"direction"`
	Text      string   `json:"text"`
	MediaURLs []string `json:"media_urls"`
	Status    string   `json:"status"`
	From      struct {
		PhoneNumber string `json:"phone_number"`
	} `json:"from"`
	To []struct {
		PhoneNumber string `json:"phone_number"`
	} `json:"to"`
	FromNumberRaw string `json:"from_number"`
	ToNumberRaw   string `json:"to_number"`
	MessageID     string `json:"message_id"`
}

// FromNumber returns the normalized sender phone number.
func (p telnyxMessagePayload) FromNumber() string {
	if v := strings.TrimSpace(p.From.PhoneNumber); v != "" {
		return v
	}
	return strings.TrimSpace(p.FromNumberRaw)
}

// ToNumber returns the normalized recipient phone number.
func (p telnyxMessagePayload) ToNumber() string {
	if len(p.To) > 0 {
		if v := strings.TrimSpace(p.To[0].PhoneNumber); v != "" {
			return v
		}
	}
	return strings.TrimSpace(p.ToNumberRaw)
}

// telnyxDeliveryPayload represents a delivery receipt from Telnyx.
type telnyxDeliveryPayload struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
	To        []struct {
		Status string `json:"status"`
	} `json:"to"`
	Errors []struct {
		Code   string `json:"code"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	} `json:"errors"`
}

// telnyxCallPayload represents a voice call event payload from Telnyx.
type telnyxCallPayload struct {
	ID            string          `json:"id"`
	Status        string          `json:"status"`
	HangupCause   string          `json:"hangup_cause"`
	FromRaw       json.RawMessage `json:"from"`
	ToRaw         json.RawMessage `json:"to"`
	FromNumberRaw string          `json:"from_number"`
	ToNumberRaw   string          `json:"to_number"`
}

// FromNumber returns the normalized caller phone number.
func (p telnyxCallPayload) FromNumber() string {
	if v := strings.TrimSpace(parseTelnyxPhone(p.FromRaw)); v != "" {
		return v
	}
	return strings.TrimSpace(p.FromNumberRaw)
}

// ToNumber returns the normalized called phone number.
func (p telnyxCallPayload) ToNumber() string {
	if v := strings.TrimSpace(parseTelnyxPhone(p.ToRaw)); v != "" {
		return v
	}
	return strings.TrimSpace(p.ToNumberRaw)
}

// parseTelnyxPhone extracts a phone number from a Telnyx JSON field that may
// be a string, an object with phone_number, or an array of objects.
func parseTelnyxPhone(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		PhoneNumber string `json:"phone_number"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.PhoneNumber
	}
	var arr []struct {
		PhoneNumber string `json:"phone_number"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return arr[0].PhoneNumber
	}
	return ""
}

// telnyxHostedPayload represents a hosted number order event payload.
type telnyxHostedPayload struct {
	ID          string `json:"id"`
	ClinicID    string `json:"clinic_id"`
	PhoneNumber string `json:"phone_number"`
	Status      string `json:"status"`
	LastError   string `json:"last_error"`
}

// telnyxConversationID builds a deterministic conversation identifier from
// the clinic org ID and the caller's E.164 number.
func telnyxConversationID(orgID string, fromE164 string) string {
	digits := sanitizeDigits(fromE164)
	digits = normalizeUSDigits(digits)
	return fmt.Sprintf("sms:%s:%s", orgID, digits)
}

// sanitizeDigits strips all non-digit characters from a string.
func sanitizeDigits(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeUSDigits prepends a leading "1" to 10-digit US phone numbers.
func normalizeUSDigits(digits string) string {
	if len(digits) == 10 {
		return "1" + digits
	}
	return digits
}

// It delegates to messaging.NormalizeE164.
