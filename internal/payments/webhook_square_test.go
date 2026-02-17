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
	"sync"
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

func TestSquareWebhookHandler_UsesFromNumberMetadata(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()

	payments := &stubPaymentStore{}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000"},
	}
	processed := &stubProcessedTracker{}
	outbox := &stubOutboxWriter{}

	handler := NewSquareWebhookHandler("secret", payments, leadsRepo, processed, outbox, nil, nil, logging.Default())

	body := buildSquarePayload(t, "evt-124", "pay-124", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
		"from_number":       "+16667778888",
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
	if outbox.inserted[0].FromNumber != "+16667778888" {
		t.Fatalf("expected from_number from metadata, got %q", outbox.inserted[0].FromNumber)
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

func TestSquareWebhookHandler_DedupesByProviderRef(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()

	payments := &stubPaymentStore{}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000"},
	}
	processed := &stubProcessedTracker{}
	outbox := &stubOutboxWriter{}
	handler := NewSquareWebhookHandler("secret", payments, leadsRepo, processed, outbox, nil, nil, logging.Default())

	body1 := buildSquarePayload(t, "evt-1", "pay-dup", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
	})
	req1 := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body1))
	req1.Host = "example.com"
	sign(req1, "secret", body1)
	rr1 := httptest.NewRecorder()
	handler.Handle(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr1.Code)
	}

	body2 := buildSquarePayload(t, "evt-2", "pay-dup", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
	})
	req2 := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body2))
	req2.Host = "example.com"
	sign(req2, "secret", body2)
	rr2 := httptest.NewRecorder()
	handler.Handle(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr2.Code)
	}

	if payments.calls != 1 {
		t.Fatalf("expected payment update once, got %d", payments.calls)
	}
	if len(outbox.inserted) != 1 {
		t.Fatalf("expected one outbox insert, got %d", len(outbox.inserted))
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
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bad signature, got %d", rr.Code)
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
	calls  int
	pay    *paymentsql.Payment
}

func (s *stubPaymentStore) UpdateStatusByID(ctx context.Context, id uuid.UUID, status, providerRef string) (*paymentsql.Payment, error) {
	s.called = true
	s.calls++
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

func (s *stubLeadRepo) ListByOrg(context.Context, string, leads.ListLeadsFilter) ([]*leads.Lead, error) {
	return nil, nil
}

func (s *stubLeadRepo) UpdateSelectedAppointment(context.Context, string, leads.SelectedAppointment) error {
	return nil
}

func (s *stubLeadRepo) UpdateBookingSession(context.Context, string, leads.BookingSessionUpdate) error {
	return nil
}

func (s *stubLeadRepo) GetByBookingSessionID(context.Context, string) (*leads.Lead, error) {
	return nil, nil
}

func (s *stubLeadRepo) UpdateEmail(context.Context, string, string) error {
	return nil
}

func (s *stubLeadRepo) ClearSelectedAppointment(context.Context, string) error {
	return nil
}

type stubProcessedTracker struct {
	already bool
	marked  bool
	mu      sync.Mutex
	seen    map[string]bool
}

func (s *stubProcessedTracker) AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	if s.already {
		return true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		return false, nil
	}
	key := provider + ":" + eventID
	return s.seen[key], nil
}

func (s *stubProcessedTracker) MarkProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marked = true
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	key := provider + ":" + eventID
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
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
