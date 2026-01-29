package messaging

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestInstantAckMessageForClinic(t *testing.T) {
	if got := InstantAckMessageForClinic(""); got != InstantAckMessage {
		t.Fatalf("expected default instant ack, got %q", got)
	}
	got := InstantAckMessageForClinic("Glow Med Spa")
	if !strings.Contains(got, "Glow Med Spa") {
		t.Fatalf("expected clinic name in ack, got %q", got)
	}
}

func TestGetSmsAckMessage(t *testing.T) {
	first := GetSmsAckMessage(true)
	if !containsString(smsAckMessagesFirst, first) {
		t.Fatalf("unexpected first ack %q", first)
	}
	if !strings.Contains(strings.ToLower(first), "medical advice") {
		t.Fatalf("expected medical advice note in first ack, got %q", first)
	}
	follow := GetSmsAckMessage(false)
	if !containsString(smsAckMessagesFollowUp, follow) {
		t.Fatalf("unexpected follow-up ack %q", follow)
	}
	if strings.Contains(strings.ToLower(follow), "medical advice") {
		t.Fatalf("did not expect medical advice note in follow-up ack, got %q", follow)
	}
}

func TestIsSmsAckMessage(t *testing.T) {
	if !IsSmsAckMessage(SmsAckMessageFirst) {
		t.Fatalf("expected first ack to be recognized")
	}
	if !IsSmsAckMessage(smsAckMessagesFollowUp[0]) {
		t.Fatalf("expected follow-up ack to be recognized")
	}
	if IsSmsAckMessage("not an ack") {
		t.Fatalf("did not expect non-ack to match")
	}
	if IsSmsAckMessage("") {
		t.Fatalf("did not expect empty string to match")
	}
}

func TestNormalizeE164(t *testing.T) {
	if got := NormalizeE164(" +1 (555) 123-4567 "); got != "+15551234567" {
		t.Fatalf("unexpected normalized phone %q", got)
	}
	if got := NormalizeE164("+15551234567"); got != "+15551234567" {
		t.Fatalf("unexpected normalized phone %q", got)
	}
	if got := NormalizeE164(" "); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
	if got := NormalizeE164("abc"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestSanitizePhone(t *testing.T) {
	if got := sanitizePhone(" +1 (555) 123-4567 "); got != "15551234567" {
		t.Fatalf("unexpected digits %q", got)
	}
	if got := sanitizePhone(""); got != "" {
		t.Fatalf("expected empty digits, got %q", got)
	}
}

func TestNormalizeE164Internal(t *testing.T) {
	if got := normalizeE164("1555"); got != "+1555" {
		t.Fatalf("unexpected normalized value %q", got)
	}
	if got := normalizeE164("+1555"); got != "+1555" {
		t.Fatalf("unexpected normalized value %q", got)
	}
	if got := normalizeE164(" "); got != "" {
		t.Fatalf("expected empty value, got %q", got)
	}
}

func TestResolvePreferredOrder(t *testing.T) {
	order := resolvePreferredOrder(SMSProviderTelnyx)
	if len(order) != 1 || order[0] != SMSProviderTelnyx {
		t.Fatalf("unexpected order %v", order)
	}
	order = resolvePreferredOrder(SMSProviderTwilio)
	if len(order) != 1 || order[0] != SMSProviderTwilio {
		t.Fatalf("unexpected order %v", order)
	}
	order = resolvePreferredOrder("auto")
	if len(order) != 2 || order[0] != SMSProviderTelnyx || order[1] != SMSProviderTwilio {
		t.Fatalf("unexpected order %v", order)
	}
}

func TestBuildReplyMessengerMissingCredentials(t *testing.T) {
	_, provider, reason := BuildReplyMessenger(ProviderSelectionConfig{
		Preference:      SMSProviderTelnyx,
		TelnyxAPIKey:    "",
		TelnyxProfileID: "",
	}, nil)
	if provider != "" {
		t.Fatalf("expected no provider, got %q", provider)
	}
	if reason == "" || !strings.Contains(reason, "TELNYX_API_KEY") {
		t.Fatalf("expected missing telnyx reason, got %q", reason)
	}

	_, provider, reason = BuildReplyMessenger(ProviderSelectionConfig{
		Preference:       SMSProviderTwilio,
		TwilioAccountSID: "",
		TwilioAuthToken:  "",
	}, nil)
	if provider != "" {
		t.Fatalf("expected no provider, got %q", provider)
	}
	if reason == "" || !strings.Contains(reason, "TWILIO_ACCOUNT_SID") {
		t.Fatalf("expected missing twilio reason, got %q", reason)
	}
}

func TestBuildReplyMessengerAutoFailover(t *testing.T) {
	messenger, provider, reason := BuildReplyMessenger(ProviderSelectionConfig{
		Preference:       SMSProviderAuto,
		TelnyxAPIKey:     "telnyx-key",
		TelnyxProfileID:  "telnyx-profile",
		TwilioAccountSID: "twilio-sid",
		TwilioAuthToken:  "twilio-token",
		TwilioFromNumber: "+15550001111",
	}, nil)

	if messenger == nil {
		t.Fatalf("expected messenger, got nil")
	}
	if provider != "telnyx+twilio" {
		t.Fatalf("expected telnyx+twilio provider, got %q", provider)
	}
	if reason != "" {
		t.Fatalf("unexpected reason %q", reason)
	}
}

func TestIsMissedCallStatus(t *testing.T) {
	if !isMissedCallStatus("no-answer") {
		t.Fatalf("expected missed call status")
	}
	if isMissedCallStatus("completed") {
		t.Fatalf("did not expect missed call status")
	}
}

func TestDeterministicIDs(t *testing.T) {
	leadID := deterministicLeadID("org-1", "+1 (555) 123-4567")
	if leadID != "org-1:15551234567" {
		t.Fatalf("unexpected lead id %q", leadID)
	}
	convID := deterministicConversationID("org-1", "+1 (555) 123-4567")
	if convID != "sms:org-1:15551234567" {
		t.Fatalf("unexpected conversation id %q", convID)
	}
}

func TestTwilioMessageUUID(t *testing.T) {
	if got := twilioMessageUUID(""); got != "" {
		t.Fatalf("expected empty uuid, got %q", got)
	}
	first := twilioMessageUUID("SM123")
	second := twilioMessageUUID("SM123")
	if first != second {
		t.Fatalf("expected stable uuid, got %q vs %q", first, second)
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("expected valid uuid, got %q", first)
	}
	if first == twilioMessageUUID("SM124") {
		t.Fatalf("expected different uuid for different sid")
	}
}

func TestBuildAbsoluteURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://internal.example.com/hook?x=1", nil)
	got := buildAbsoluteURL(req, "https://public.example.com/api/")
	if got != "https://public.example.com/api/hook?x=1" {
		t.Fatalf("unexpected absolute url %q", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/hook", nil)
	req.Host = "internal.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "forward.example.com")
	got = buildAbsoluteURL(req, "")
	if got != "https://forward.example.com/hook" {
		t.Fatalf("unexpected forwarded url %q", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/hook", nil)
	req.Host = "internal.example.com"
	req.TLS = &tls.ConnectionState{}
	got = buildAbsoluteURL(req, "")
	if got != "https://internal.example.com/hook" {
		t.Fatalf("unexpected tls url %q", got)
	}
}

func containsString(values []string, candidate string) bool {
	for _, v := range values {
		if v == candidate {
			return true
		}
	}
	return false
}
