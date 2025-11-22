package messaging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestValidateTwilioSignature(t *testing.T) {
	authToken := "test_token"
	webhookURL := "https://example.com/webhook"

	// Create a test request
	formData := url.Values{}
	formData.Set("MessageSid", "SM123")
	formData.Set("From", "+1234567890")
	formData.Set("Body", "Hello")

	req := httptest.NewRequest(http.MethodPost, webhookURL, strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Compute expected signature
	payload := buildSignaturePayload(webhookURL, formData)
	expectedSignature := computeSignature(payload, authToken)
	req.Header.Set("X-Twilio-Signature", expectedSignature)

	if !ValidateTwilioSignature(req, authToken, webhookURL) {
		t.Error("expected signature validation to pass")
	}
}

func TestValidateTwilioSignature_InvalidSignature(t *testing.T) {
	authToken := "test_token"
	webhookURL := "https://example.com/webhook"

	formData := url.Values{}
	formData.Set("MessageSid", "SM123")

	req := httptest.NewRequest(http.MethodPost, webhookURL, strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", "invalid_signature")

	if ValidateTwilioSignature(req, authToken, webhookURL) {
		t.Error("expected signature validation to fail")
	}
}

func TestValidateTwilioSignature_MissingSignature(t *testing.T) {
	authToken := "test_token"
	webhookURL := "https://example.com/webhook"

	formData := url.Values{}
	formData.Set("MessageSid", "SM123")

	req := httptest.NewRequest(http.MethodPost, webhookURL, strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if ValidateTwilioSignature(req, authToken, webhookURL) {
		t.Error("expected signature validation to fail without signature header")
	}
}

func TestParseTwilioWebhook(t *testing.T) {
	formData := url.Values{}
	formData.Set("MessageSid", "SM123")
	formData.Set("AccountSid", "AC456")
	formData.Set("From", "+1234567890")
	formData.Set("To", "+0987654321")
	formData.Set("Body", "Test message")
	formData.Set("NumMedia", "0")

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	webhook, err := ParseTwilioWebhook(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if webhook.MessageSid != "SM123" {
		t.Errorf("expected MessageSid SM123, got %s", webhook.MessageSid)
	}

	if webhook.From != "+1234567890" {
		t.Errorf("expected From +1234567890, got %s", webhook.From)
	}

	if webhook.Body != "Test message" {
		t.Errorf("expected Body 'Test message', got %s", webhook.Body)
	}
}

func TestTwilioWebhookHandler(t *testing.T) {
	handler, pub := newTestHandler(t, "", nil, nil)

	formData := url.Values{}
	formData.Set("MessageSid", "SM123")
	formData.Set("From", "+1234567890")
	formData.Set("To", "+15551234567")
	formData.Set("Body", "Hello")

	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.TwilioWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/xml" {
		t.Errorf("expected Content-Type application/xml, got %s", contentType)
	}
	if !pub.called {
		t.Fatalf("expected publisher to be called")
	}
	if pub.lastReq.OrgID != "org-test" {
		t.Fatalf("expected org-test, got %s", pub.lastReq.OrgID)
	}
}

func TestTwilioWebhookHandler_WithSignatureValidation(t *testing.T) {
	authToken := "test_secret"
	handler, _ := newTestHandler(t, authToken, nil, nil)

	formData := url.Values{}
	formData.Set("MessageSid", "SM123")
	formData.Set("From", "+1234567890")
	formData.Set("To", "+15551234567")

	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", "invalid")

	w := httptest.NewRecorder()
	handler.TwilioWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestTwilioWebhookHandler_WithValidSignature(t *testing.T) {
	authToken := "valid_secret"
	handler, pub := newTestHandler(t, authToken, nil, nil)

	formData := url.Values{}
	formData.Set("MessageSid", "SM999")
	formData.Set("From", "+15555555555")
	formData.Set("Body", "Ping")
	formData.Set("To", "+15551234567")

	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.com"

	webhookURL := "http://example.com/messaging/twilio/webhook"
	signature := computeSignature(buildSignaturePayload(webhookURL, formData), authToken)
	req.Header.Set("X-Twilio-Signature", signature)

	w := httptest.NewRecorder()
	handler.TwilioWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if !pub.called {
		t.Fatalf("expected publisher to be called")
	}
}

func TestTwilioWebhookHandler_ParseError(t *testing.T) {
	handler, pub := newTestHandler(t, "", nil, nil)

	// Body contains invalid percent-encoding to force ParseForm error.
	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader("%"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.TwilioWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if pub.called {
		t.Fatalf("did not expect publisher call on parse error")
	}
}

func TestTwilioWebhookHandler_UnknownOrg(t *testing.T) {
	resolver := NewStaticOrgResolver(map[string]string{})
	handler, pub := newTestHandler(t, "", nil, resolver)

	formData := url.Values{}
	formData.Set("MessageSid", "SM777")
	formData.Set("From", "+15555555555")
	formData.Set("Body", "Ping")
	formData.Set("To", "+19998887777")

	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.TwilioWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown org, got %d", w.Code)
	}
	if pub.called {
		t.Fatalf("did not expect publisher call when org missing")
	}
}

func TestHealthCheck(t *testing.T) {
	handler, _ := newTestHandler(t, "", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handler.HealthCheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
}

type stubPublisher struct {
	called     bool
	lastJob    string
	lastReq    conversation.MessageRequest
	lastStart  conversation.StartRequest
	startJobID string
	err        error
}

func (s *stubPublisher) EnqueueStart(ctx context.Context, jobID string, req conversation.StartRequest, opts ...conversation.PublishOption) error {
	s.startJobID = jobID
	s.lastStart = req
	return s.err
}

func (s *stubPublisher) EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error {
	s.called = true
	s.lastJob = jobID
	s.lastReq = req
	return s.err
}

func newTestHandler(t *testing.T, secret string, pubErr error, resolver OrgResolver) (*Handler, *stubPublisher) {
	t.Helper()
	pub := &stubPublisher{err: pubErr}
	if resolver == nil {
		resolver = NewStaticOrgResolver(map[string]string{
			"+15551234567": "org-test",
		})
	}
	handler := NewHandler(secret, pub, resolver, nil, logging.Default())
	return handler, pub
}

func TestStaticResolverDefaultNumber(t *testing.T) {
	res := NewStaticOrgResolver(map[string]string{
		"(555) 123-4567": "org-a",
	})
	num := res.DefaultFromNumber("org-a")
	if num != "+5551234567" {
		t.Fatalf("expected normalized e164, got %s", num)
	}
}
