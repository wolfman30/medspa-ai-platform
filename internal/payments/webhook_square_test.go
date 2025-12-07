package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestSquareWebhookHandler_Success(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()
	scheduled := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	payments := &stubPaymentStore{}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000"},
	}
	processed := &stubProcessedTracker{}
	outbox := &stubOutboxWriter{}
	numbers := stubNumberResolver("+19998887777")

	handler := NewSquareWebhookHandler("secret", payments, leadsRepo, processed, outbox, numbers, nil, logging.Default())

	body := buildSquarePayload(t, "evt-123", "pay-123", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
		"scheduled_for":     scheduled.Format(time.RFC3339),
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
	if outbox.inserted[0].ScheduledFor == nil || !outbox.inserted[0].ScheduledFor.Equal(scheduled) {
		t.Fatalf("expected scheduled_for to propagate, got %#v", outbox.inserted[0].ScheduledFor)
	}
}

func TestSquareWebhookHandler_ScheduledFor(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()
	sched := time.Date(2025, 12, 1, 15, 0, 0, 0, time.UTC)

	payments := &stubPaymentStore{}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000"},
	}
	processed := &stubProcessedTracker{}
	outbox := &stubOutboxWriter{}

	handler := NewSquareWebhookHandler("secret", payments, leadsRepo, processed, outbox, nil, nil, logging.Default())

	body := buildSquarePayload(t, "evt-123", "pay-123", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
		"scheduled_for":     sched.Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	sign(req, "secret", body)

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(outbox.inserted) != 1 {
		t.Fatalf("expected outbox insert, got %d", len(outbox.inserted))
	}
	if outbox.inserted[0].ScheduledFor == nil || !outbox.inserted[0].ScheduledFor.Equal(sched) {
		t.Fatalf("expected scheduled_for %v, got %+v", sched, outbox.inserted[0].ScheduledFor)
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
	handler := NewSquareWebhookHandler("secret", &stubPaymentStore{}, &stubLeadRepo{}, &stubProcessedTracker{}, &stubOutboxWriter{}, nil, nil, logging.Default())
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
	pay := samplePayment(uuid.New(), "pay")
	handler := NewSquareWebhookHandler("secret", &stubPaymentStore{pay: pay}, &stubLeadRepo{
		lead: &leads.Lead{ID: uuid.New().String(), OrgID: pay.OrgID, Phone: "+15550000000"},
	}, &stubProcessedTracker{}, &stubOutboxWriter{}, nil, nil, logging.Default())
	body := buildSquarePayload(t, "evt", "pay", "COMPLETED", map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	sign(req, "secret", body)
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with provider ref fallback, got %d", rr.Code)
	}
}

func TestSquareWebhookHandler_NonCompletedStatus(t *testing.T) {
	handler := NewSquareWebhookHandler("secret", &stubPaymentStore{}, &stubLeadRepo{}, &stubProcessedTracker{}, &stubOutboxWriter{}, nil, nil, logging.Default())
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

func TestSquareWebhookHandler_FallbacksScheduledForFromPaymentRow(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()
	scheduled := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)

	pay := samplePaymentWithSchedule(uuid.MustParse(intentID), "pay-fallback", scheduled)
	payments := &stubPaymentStore{pay: pay}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000"},
	}
	processed := &stubProcessedTracker{}
	outbox := &stubOutboxWriter{}

	handler := NewSquareWebhookHandler("secret", payments, leadsRepo, processed, outbox, nil, nil, logging.Default())

	body := buildSquarePayload(t, "evt-fallback", pay.ProviderRef.String, "COMPLETED", map[string]string{
		"org_id": orgID,
		// intentionally omit scheduled_for to trigger fallback from DB row
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	sign(req, "secret", body)

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(outbox.inserted) != 1 {
		t.Fatalf("expected outbox insert, got %d", len(outbox.inserted))
	}
	got := outbox.inserted[0].ScheduledFor
	if got == nil || !got.Equal(scheduled) {
		t.Fatalf("expected scheduled_for from payment row, got %v", got)
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
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(url + string(body)))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func samplePayment(id uuid.UUID, providerRef string) *paymentsql.Payment {
	return &paymentsql.Payment{
		ID:     pgtype.UUID{Bytes: [16]byte(id), Valid: id != uuid.Nil},
		OrgID:  uuid.New().String(),
		LeadID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		ProviderRef: pgtype.Text{
			String: providerRef,
			Valid:  providerRef != "",
		},
	}
}

func samplePaymentWithSchedule(id uuid.UUID, providerRef string, scheduled time.Time) *paymentsql.Payment {
	return &paymentsql.Payment{
		ID:     pgtype.UUID{Bytes: [16]byte(id), Valid: id != uuid.Nil},
		OrgID:  uuid.New().String(),
		LeadID: pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true},
		ProviderRef: pgtype.Text{
			String: providerRef,
			Valid:  providerRef != "",
		},
		ScheduledFor: pgtype.Timestamptz{
			Time:  scheduled,
			Valid: true,
		},
	}
}

type stubPaymentStore struct {
	called bool
	pay    *paymentsql.Payment
}

func (s *stubPaymentStore) UpdateStatusByID(ctx context.Context, id uuid.UUID, status, providerRef string) (*paymentsql.Payment, error) {
	s.called = true
	if s.pay != nil {
		return s.pay, nil
	}
	return samplePayment(id, providerRef), nil
}

func (s *stubPaymentStore) GetByProviderRef(ctx context.Context, providerRef string) (*paymentsql.Payment, error) {
	if s.pay != nil {
		return s.pay, nil
	}
	return samplePayment(uuid.New(), providerRef), nil
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

func (s *stubLeadRepo) GetOrCreateByPhone(context.Context, string, string, string, string) (*leads.Lead, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.lead == nil {
		return nil, leads.ErrLeadNotFound
	}
	return s.lead, nil
}

func (s *stubLeadRepo) UpdateSchedulingPreferences(context.Context, string, leads.SchedulingPreferences) error {
	return nil
}

func (s *stubLeadRepo) UpdateDepositStatus(context.Context, string, string, string) error {
	return nil
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
