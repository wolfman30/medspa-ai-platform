package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSanitizeDigits(t *testing.T) {
	if got := sanitizeDigits(" +1 (555) 123-4567 "); got != "15551234567" {
		t.Fatalf("unexpected digits %q", got)
	}
	if got := sanitizeDigits("abc"); got != "" {
		t.Fatalf("expected empty digits, got %q", got)
	}
}

func TestNormalizeUSDigits(t *testing.T) {
	if got := normalizeUSDigits("5551234567"); got != "15551234567" {
		t.Fatalf("expected normalized digits, got %q", got)
	}
	if got := normalizeUSDigits("15551234567"); got != "15551234567" {
		t.Fatalf("expected unchanged digits, got %q", got)
	}
	if got := normalizeUSDigits("123"); got != "123" {
		t.Fatalf("expected unchanged digits, got %q", got)
	}
}

func TestTelnyxConversationID(t *testing.T) {
	convID := telnyxConversationID("org-1", "+1 (555) 123-4567")
	if convID != "sms:org-1:15551234567" {
		t.Fatalf("unexpected conversation id %q", convID)
	}
}

func TestIsTelnyxMissedCall(t *testing.T) {
	if !isTelnyxMissedCall("call.hangup", "no_answer", "") {
		t.Fatalf("expected missed call for no_answer")
	}
	if !isTelnyxMissedCall("call.hangup", "", "originator_cancel") {
		t.Fatalf("expected missed call for originator_cancel")
	}
	if !isTelnyxMissedCall("call.hangup", "", "") {
		t.Fatalf("expected missed call for hangup event type")
	}
	if isTelnyxMissedCall("call.answered", "completed", "") {
		t.Fatalf("did not expect missed call for completed")
	}
}

func TestParseTelnyxPhone(t *testing.T) {
	rawString := json.RawMessage(`"+15551234567"`)
	if got := parseTelnyxPhone(rawString); got != "+15551234567" {
		t.Fatalf("unexpected phone %q", got)
	}

	rawObject := json.RawMessage(`{"phone_number":"+15550001111"}`)
	if got := parseTelnyxPhone(rawObject); got != "+15550001111" {
		t.Fatalf("unexpected phone %q", got)
	}

	rawArray := json.RawMessage(`[{"phone_number":"+15559998888"}]`)
	if got := parseTelnyxPhone(rawArray); got != "+15559998888" {
		t.Fatalf("unexpected phone %q", got)
	}

	rawInvalid := json.RawMessage(`{"phone":"nope"}`)
	if got := parseTelnyxPhone(rawInvalid); got != "" {
		t.Fatalf("expected empty phone, got %q", got)
	}
}

func TestParseTelnyxEventWrapper(t *testing.T) {
	payload := `{"data":{"id":"evt_1","event_type":"message.received","occurred_at":"2025-01-01T00:00:00Z","payload":{"foo":"bar"}}}`
	evt, err := parseTelnyxEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.ID != "evt_1" || evt.EventType != "message.received" {
		t.Fatalf("unexpected event data: %#v", evt)
	}
	if evt.OccurredAt.Format(time.RFC3339) != "2025-01-01T00:00:00Z" {
		t.Fatalf("unexpected occurred_at %s", evt.OccurredAt.Format(time.RFC3339))
	}
	if !strings.Contains(string(evt.Payload), "\"foo\"") {
		t.Fatalf("expected payload to include foo, got %s", string(evt.Payload))
	}
}

func TestParseTelnyxEventRecordInbound(t *testing.T) {
	payload := `{"id":"msg_1","record_type":"message","received_at":"2025-01-02T03:04:05Z","direction":"inbound"}`
	evt, err := parseTelnyxEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.EventType != "message.received" {
		t.Fatalf("unexpected event type %q", evt.EventType)
	}
	if evt.ID != "msg_1" {
		t.Fatalf("unexpected id %q", evt.ID)
	}
	if !strings.Contains(string(evt.Payload), "\"record_type\"") {
		t.Fatalf("expected payload to be original record")
	}
}

func TestParseTelnyxEventRecordOutbound(t *testing.T) {
	payload := `{"id":"msg_2","record_type":"message","received_at":"2025-01-02T03:04:05Z","direction":"outbound"}`
	evt, err := parseTelnyxEvent([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.EventType != "message.delivery_status" {
		t.Fatalf("unexpected event type %q", evt.EventType)
	}
}

func TestParseTelnyxEventInvalidJSON(t *testing.T) {
	if _, err := parseTelnyxEvent([]byte("nope")); err == nil {
		t.Fatalf("expected error for invalid json")
	}
}

func TestIsDuplicateProviderMessage(t *testing.T) {
	if isDuplicateProviderMessage(nil) {
		t.Fatalf("expected false for nil error")
	}
	if !isDuplicateProviderMessage(&pgconn.PgError{Code: "23505", ConstraintName: "idx_messages_provider_message"}) {
		t.Fatalf("expected duplicate for unique violation")
	}
	if !isDuplicateProviderMessage(&pgconn.PgError{Code: "23505"}) {
		t.Fatalf("expected duplicate when constraint name missing")
	}
	if isDuplicateProviderMessage(&pgconn.PgError{Code: "99999"}) {
		t.Fatalf("expected false for non-unique error")
	}
}
