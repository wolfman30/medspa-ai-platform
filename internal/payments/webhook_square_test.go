package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestSquareWebhookHandler_Success(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()

	payments := &stubPaymentStore{}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000"},
	}
	processed := &stubProcessedTracker{}
	outbox := &stubOutboxWriter{}
	numbers := stubNumberResolver("+19998887777")

	handler := NewSquareWebhookHandler("secret", payments, leadsRepo, processed, outbox, numbers, logging.Default())

	body := buildSquarePayload(t, "evt-123", "pay-123", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	sign(req, "secret", body)

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !payments.called {
		t.Fatalf("expected payment update to run")
	}
	if len(outbox.inserted) != 1 {
		t.Fatalf("expected outbox insert, got %d", len(outbox.inserted))
	}
	if !processed.marked {
		t.Fatalf("expected processed marker to run")
	}
	if outbox.inserted[0].FromNumber != "+19998887777" {
		t.Fatalf("expected from number injection")
	}
}

func TestSquareWebhookHandler_AlreadyProcessed(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()

	handler := NewSquareWebhookHandler(
		"secret",
		&stubPaymentStore{},
		&stubLeadRepo{},
		&stubProcessedTracker{already: true},
		&stubOutboxWriter{},
		nil,
		logging.Default(),
	)

	body := buildSquarePayload(t, "evt-abc", "pay-abc", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	sign(req, "secret", body)
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for duplicate, got %d", rr.Code)
	}
}

func TestSquareWebhookHandler_InvalidSignature(t *testing.T) {
	handler := NewSquareWebhookHandler("secret", &stubPaymentStore{}, &stubLeadRepo{}, &stubProcessedTracker{}, &stubOutboxWriter{}, nil, logging.Default())
	body := buildSquarePayload(t, "evt", "pay", "COMPLETED", map[string]string{
		"org_id":            uuid.New().String(),
		"lead_id":           uuid.New().String(),
		"booking_intent_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	req.Header.Set("X-Square-Signature", "bad")

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad signature, got %d", rr.Code)
	}
}

func TestSquareWebhookHandler_MissingMetadata(t *testing.T) {
	handler := NewSquareWebhookHandler("secret", &stubPaymentStore{}, &stubLeadRepo{}, &stubProcessedTracker{}, &stubOutboxWriter{}, nil, logging.Default())
	body := buildSquarePayload(t, "evt", "pay", "COMPLETED", map[string]string{
		"org_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	sign(req, "secret", body)
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing metadata, got %d", rr.Code)
	}
}

func TestSquareWebhookHandler_NonCompletedStatus(t *testing.T) {
	handler := NewSquareWebhookHandler("secret", &stubPaymentStore{}, &stubLeadRepo{}, &stubProcessedTracker{}, &stubOutboxWriter{}, nil, logging.Default())
	body := buildSquarePayload(t, "evt", "pay", "PENDING", map[string]string{
		"org_id":            uuid.New().String(),
		"lead_id":           uuid.New().String(),
		"booking_intent_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	sign(req, "secret", body)
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for ignored status, got %d", rr.Code)
	}
}

func TestVerifySquareSignature(t *testing.T) {
	body := []byte(`{"ping":true}`)
	url := "http://example.com/webhooks/square"
	key := "secret"
	sig := computeSignature(key, url, body)
	if !verifySquareSignature(key, url, body, sig) {
		t.Fatal("expected signature to validate")
	}
	if verifySquareSignature(key, url, body, "bad") {
		t.Fatal("expected invalid signature to fail")
	}
}

// Helpers & stubs

func buildSquarePayload(t *testing.T, eventID, paymentID, status string, metadata map[string]string) []byte {
	t.Helper()
	evt := squarePaymentEvent{
		ID:        eventID,
		CreatedAt: time.Now().UTC(),
	}
	evt.Data.Object.Payment.ID = paymentID
	evt.Data.Object.Payment.Status = status
	evt.Data.Object.Payment.AmountMoney.Amount = 5000
	evt.Data.Object.Payment.AmountMoney.Currency = "USD"
	evt.Data.Object.Payment.Metadata = metadata
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal event: %v", err)
	}
	return data
}

func sign(req *http.Request, key string, body []byte) {
	url := buildAbsoluteURL(req)
	req.Header.Set("X-Square-Signature", computeSignature(key, url, body))
}

func computeSignature(key, url string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(url + string(body)))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

type stubPaymentStore struct {
	called bool
}

func (s *stubPaymentStore) UpdateStatusByID(ctx context.Context, id uuid.UUID, status, providerRef string) (*paymentsql.Payment, error) {
	s.called = true
	return &paymentsql.Payment{}, nil
}

type stubLeadRepo struct {
	lead *leads.Lead
	err  error
}

func (s *stubLeadRepo) Create(context.Context, *leads.CreateLeadRequest) (*leads.Lead, error) {
	return nil, errors.New("not implemented")
}

func (s *stubLeadRepo) GetByID(ctx context.Context, orgID string, id string) (*leads.Lead, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.lead == nil {
		return nil, leads.ErrLeadNotFound
	}
	return s.lead, nil
}

type stubProcessedTracker struct {
	already bool
	marked  bool
}

func (s *stubProcessedTracker) AlreadyProcessed(context.Context, string, string) (bool, error) {
	return s.already, nil
}

func (s *stubProcessedTracker) MarkProcessed(context.Context, string, string) (bool, error) {
	s.marked = true
	return true, nil
}

type stubOutboxWriter struct {
	inserted []events.PaymentSucceededV1
}

func (s *stubOutboxWriter) Insert(ctx context.Context, orgID string, eventType string, payload any) (uuid.UUID, error) {
	if evt, ok := payload.(events.PaymentSucceededV1); ok {
		s.inserted = append(s.inserted, evt)
	}
	return uuid.New(), nil
}

type stubNumberResolver string

func (s stubNumberResolver) DefaultFromNumber(string) string {
	return string(s)
}
