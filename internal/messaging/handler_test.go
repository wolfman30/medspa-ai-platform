package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
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

type stubLeadsRepo struct {
	called    bool
	lastOrg   string
	lastPhone string
	lastSrc   string
	lead      *leads.Lead
	err       error
}

func (s *stubLeadsRepo) Create(context.Context, *leads.CreateLeadRequest) (*leads.Lead, error) {
	return nil, errors.New("not implemented")
}

func (s *stubLeadsRepo) GetByID(context.Context, string, string) (*leads.Lead, error) {
	return nil, errors.New("not implemented")
}

func (s *stubLeadsRepo) GetOrCreateByPhone(ctx context.Context, orgID string, phone string, source string, defaultName string) (*leads.Lead, error) {
	s.called = true
	s.lastOrg = orgID
	s.lastPhone = phone
	s.lastSrc = source
	if s.err != nil {
		return nil, s.err
	}
	if s.lead != nil {
		return s.lead, nil
	}
	return &leads.Lead{ID: "lead-stub", OrgID: orgID, Phone: phone, Source: source}, nil
}

func (s *stubLeadsRepo) UpdateSchedulingPreferences(context.Context, string, leads.SchedulingPreferences) error {
	return nil
}

func (s *stubLeadsRepo) UpdateDepositStatus(context.Context, string, string, string) error {
	return nil
}

func (s *stubLeadsRepo) ListByOrg(context.Context, string, leads.ListLeadsFilter) ([]*leads.Lead, error) {
	return nil, nil
}

func newTestHandler(t *testing.T, secret string, pubErr error, resolver OrgResolver) (*Handler, *stubPublisher) {
	t.Helper()
	pub := &stubPublisher{err: pubErr}
	if resolver == nil {
		resolver = NewStaticOrgResolver(map[string]string{
			"+15551234567": "org-test",
		})
	}
	handler := NewHandler(secret, pub, resolver, nil, leads.NewInMemoryRepository(), logging.Default())
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

func TestTwilioWebhook_UpsertsLead(t *testing.T) {
	resolver := NewStaticOrgResolver(map[string]string{
		"+15550001111": "org-test",
	})
	pub := &stubPublisher{}
	leadRepo := &stubLeadsRepo{lead: &leads.Lead{ID: "lead-123", OrgID: "org-test"}}
	handler := NewHandler("", pub, resolver, nil, leadRepo, logging.Default())

	formData := url.Values{}
	formData.Set("MessageSid", "SM123")
	formData.Set("AccountSid", "AC123")
	formData.Set("From", "+15559998888")
	formData.Set("To", "+15550001111")
	formData.Set("Body", "Hi there")

	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.TwilioWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if !leadRepo.called {
		t.Fatalf("expected lead repo to be invoked")
	}
	if leadRepo.lastOrg != "org-test" || leadRepo.lastPhone != "+15559998888" || leadRepo.lastSrc != "twilio_sms" {
		t.Fatalf("unexpected lead args org=%s phone=%s src=%s", leadRepo.lastOrg, leadRepo.lastPhone, leadRepo.lastSrc)
	}
	if pub.lastReq.LeadID != "lead-123" {
		t.Fatalf("expected conversation lead id to match repo result, got %s", pub.lastReq.LeadID)
	}
}

type stubMessenger struct {
	called bool
	last   conversation.OutboundReply
	err    error
}

func (s *stubMessenger) SendReply(ctx context.Context, reply conversation.OutboundReply) error {
	s.called = true
	s.last = reply
	return s.err
}

func TestTwilioWebhook_SendsAckSMS(t *testing.T) {
	resolver := NewStaticOrgResolver(map[string]string{
		"+15550001111": "org-test",
	})
	pub := &stubPublisher{}
	leadRepo := &stubLeadsRepo{lead: &leads.Lead{
		ID:        "lead-123",
		OrgID:     "org-test",
		CreatedAt: time.Now().UTC(),
	}}
	messenger := &stubMessenger{}
	handler := NewHandler("", pub, resolver, messenger, leadRepo, logging.Default())

	formData := url.Values{}
	formData.Set("MessageSid", "SM123")
	formData.Set("AccountSid", "AC123")
	formData.Set("From", "+15559998888")
	formData.Set("To", "+15550001111")
	formData.Set("Body", "Hi there")

	req := httptest.NewRequest(http.MethodPost, "/messaging/twilio/webhook", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.TwilioWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	if !messenger.called {
		t.Fatalf("expected ack SMS to be sent")
	}
	if messenger.last.To != "+15559998888" {
		t.Fatalf("expected ack to=%s, got %s", "+15559998888", messenger.last.To)
	}
	if messenger.last.From != "+15550001111" {
		t.Fatalf("expected ack from=%s, got %s", "+15550001111", messenger.last.From)
	}
	if messenger.last.Body != SmsAckMessageFirst {
		t.Fatalf("expected ack body %q, got %q", SmsAckMessageFirst, messenger.last.Body)
	}
}
