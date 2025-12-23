package conversation

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func TestTierA_CI18_SquareSignatureFailure(t *testing.T) {
	orgID := uuid.New().String()
	leadRepo := leads.NewInMemoryRepository()
	lead, err := leadRepo.Create(context.Background(), &leads.CreateLeadRequest{
		OrgID:   orgID,
		Name:    "Test Lead",
		Phone:   "+15550000000",
		Source:  "sms",
		Message: "",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	handler := payments.NewSquareWebhookHandler(
		"secret",
		&stubPaymentStatusStore{},
		leadRepo,
		&stubProcessedTracker{},
		&stubOutboxWriter{},
		&stubOrgNumberResolver{from: "+15550000000"},
		nil,
		logging.Default(),
	)

	body := buildSquarePayload(t, "evt-bad-sig", "pay-1", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           lead.ID,
		"booking_intent_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	req.Header.Set("X-Square-Signature", "bad")
	rr := httptest.NewRecorder()

	handler.Handle(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestTierA_CI09_SquareWebhookPaid_ConfirmationSMS(t *testing.T) {
	ctx := context.Background()

	orgID := uuid.New().String()
	leadRepo := leads.NewInMemoryRepository()
	lead, err := leadRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   orgID,
		Name:    "Test Lead",
		Phone:   "+15550000000",
		Source:  "sms",
		Message: "",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	outbox := &stubOutboxWriter{}
	processed := &stubProcessedTracker{}
	numbers := &stubOrgNumberResolver{from: "+15551112222"}
	handler := payments.NewSquareWebhookHandler("secret", &stubPaymentStatusStore{}, leadRepo, processed, outbox, numbers, nil, logging.Default())

	intentID := uuid.New().String()
	scheduled := time.Date(2025, 1, 2, 15, 4, 0, 0, time.UTC)
	body := buildSquarePayload(t, "evt-paid", "pay-1", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           lead.ID,
		"booking_intent_id": intentID,
		"scheduled_for":     scheduled.Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	signSquare(req, "secret", body)
	rr := httptest.NewRecorder()
	handler.Handle(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	updated, err := leadRepo.GetByID(ctx, orgID, lead.ID)
	if err != nil {
		t.Fatalf("get lead: %v", err)
	}
	if updated.DepositStatus != "paid" || updated.PriorityLevel != "priority" {
		t.Fatalf("expected lead deposit_status paid/priority, got status=%q priority=%q", updated.DepositStatus, updated.PriorityLevel)
	}
	if len(outbox.inserted) != 1 || outbox.inserted[0].eventType != "payment_succeeded.v1" {
		t.Fatalf("expected one payment_succeeded.v1 outbox insert, got %#v", outbox.inserted)
	}

	// Dispatch outbox -> worker -> confirmation SMS
	queue := NewMemoryQueue(8)
	jobs := &stubJobUpdater{}
	publisher := NewPublisher(queue, &stubJobRecorder{}, logging.Default())
	dispatcher := NewOutboxDispatcher(publisher)

	workerMessenger := &stubMessenger{}
	bookings := &stubBookingConfirmer{}
	workerProcessed := &stubProcessedStore{seen: map[string]bool{}}
	worker := NewWorker(NewStubService(), queue, jobs, workerMessenger, bookings, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0), WithProcessedEventsStore(workerProcessed))

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	worker.Start(runCtx)

	entryPayload, err := json.Marshal(outbox.inserted[0].payload)
	if err != nil {
		t.Fatalf("marshal outbox payload: %v", err)
	}
	entry := events.OutboxEntry{ID: uuid.New(), Aggregate: orgID, EventType: "payment_succeeded.v1", Payload: entryPayload, CreatedAt: time.Now().UTC()}
	if err := dispatcher.Handle(ctx, entry); err != nil {
		t.Fatalf("dispatch outbox: %v", err)
	}

	waitFor(func() bool {
		return workerMessenger.wasCalled()
	}, time.Second, t)
	cancel()
	worker.Wait()

	last := workerMessenger.lastReply()
	if last.To != lead.Phone || last.From != numbers.from {
		t.Fatalf("unexpected to/from: %#v", last)
	}
	if !strings.Contains(last.Body, scheduled.Format("Monday, January 2 at 3:04 PM")) {
		t.Fatalf("expected scheduled time in confirmation sms, got %q", last.Body)
	}
}

func TestTierA_CI10_SquareWebhookIdempotency_NoDuplicateOutbox(t *testing.T) {
	ctx := context.Background()

	orgID := uuid.New().String()
	leadRepo := leads.NewInMemoryRepository()
	lead, err := leadRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   orgID,
		Name:    "Test Lead",
		Phone:   "+15550000000",
		Source:  "sms",
		Message: "",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	outbox := &stubOutboxWriter{}
	processed := &stubProcessedTracker{}
	handler := payments.NewSquareWebhookHandler("secret", &stubPaymentStatusStore{}, leadRepo, processed, outbox, &stubOrgNumberResolver{from: "+15551112222"}, nil, logging.Default())

	intentID := uuid.New().String()
	body1 := buildSquarePayload(t, "evt-1", "pay-dup", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           lead.ID,
		"booking_intent_id": intentID,
	})
	req1 := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body1))
	req1.Host = "example.com"
	signSquare(req1, "secret", body1)
	rr1 := httptest.NewRecorder()
	handler.Handle(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr1.Code)
	}

	// Retry with a new webhook event id but the same Square payment id.
	body2 := buildSquarePayload(t, "evt-2", "pay-dup", "COMPLETED", map[string]string{
		"org_id":            orgID,
		"lead_id":           lead.ID,
		"booking_intent_id": intentID,
	})
	req2 := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body2))
	req2.Host = "example.com"
	signSquare(req2, "secret", body2)
	rr2 := httptest.NewRecorder()
	handler.Handle(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr2.Code)
	}

	if len(outbox.inserted) != 1 {
		t.Fatalf("expected one outbox insert, got %d", len(outbox.inserted))
	}
}

func TestTierA_CI21_PaymentFailure_UpdatesLeadAndSendsSMS(t *testing.T) {
	ctx := context.Background()

	orgID := uuid.New().String()
	leadRepo := leads.NewInMemoryRepository()
	lead, err := leadRepo.Create(ctx, &leads.CreateLeadRequest{
		OrgID:   orgID,
		Name:    "Test Lead",
		Phone:   "+15550000000",
		Source:  "sms",
		Message: "",
	})
	if err != nil {
		t.Fatalf("create lead: %v", err)
	}

	outbox := &stubOutboxWriter{}
	processed := &stubProcessedTracker{}
	numbers := &stubOrgNumberResolver{from: "+15551112222"}
	handler := payments.NewSquareWebhookHandler("secret", &stubPaymentStatusStore{}, leadRepo, processed, outbox, numbers, nil, logging.Default())

	intentID := uuid.New().String()
	body := buildSquarePayload(t, "evt-fail", "pay-fail", "FAILED", map[string]string{
		"org_id":            orgID,
		"lead_id":           lead.ID,
		"booking_intent_id": intentID,
	})
	req := httptest.NewRequest(http.MethodPost, "http://example.com/webhooks/square", bytes.NewReader(body))
	req.Host = "example.com"
	signSquare(req, "secret", body)
	rr := httptest.NewRecorder()
	handler.Handle(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	updated, err := leadRepo.GetByID(ctx, orgID, lead.ID)
	if err != nil {
		t.Fatalf("get lead: %v", err)
	}
	if updated.DepositStatus != "failed" {
		t.Fatalf("expected lead deposit_status failed, got %q", updated.DepositStatus)
	}
	if len(outbox.inserted) != 1 || outbox.inserted[0].eventType != "payment_failed.v1" {
		t.Fatalf("expected one payment_failed.v1 outbox insert, got %#v", outbox.inserted)
	}

	// Dispatch outbox -> worker -> failure SMS
	queue := NewMemoryQueue(8)
	jobs := &stubJobUpdater{}
	publisher := NewPublisher(queue, &stubJobRecorder{}, logging.Default())
	dispatcher := NewOutboxDispatcher(publisher)

	workerMessenger := &stubMessenger{}
	workerProcessed := &stubProcessedStore{seen: map[string]bool{}}
	worker := NewWorker(NewStubService(), queue, jobs, workerMessenger, nil, logging.Default(), WithWorkerCount(1), WithReceiveBatchSize(1), WithReceiveWaitSeconds(0), WithProcessedEventsStore(workerProcessed))

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	worker.Start(runCtx)

	entryPayload, err := json.Marshal(outbox.inserted[0].payload)
	if err != nil {
		t.Fatalf("marshal outbox payload: %v", err)
	}
	entry := events.OutboxEntry{ID: uuid.New(), Aggregate: orgID, EventType: "payment_failed.v1", Payload: entryPayload, CreatedAt: time.Now().UTC()}
	if err := dispatcher.Handle(ctx, entry); err != nil {
		t.Fatalf("dispatch outbox: %v", err)
	}

	waitFor(func() bool {
		return workerMessenger.wasCalled()
	}, time.Second, t)
	cancel()
	worker.Wait()

	last := workerMessenger.lastReply()
	if last.To != lead.Phone || last.From != numbers.from {
		t.Fatalf("unexpected to/from: %#v", last)
	}
	if !strings.Contains(strings.ToLower(last.Body), "payment failed") {
		t.Fatalf("expected payment failed sms, got %q", last.Body)
	}
}

type stubProcessedTracker struct {
	seen map[string]bool
}

func (s *stubProcessedTracker) AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	return s.seen[provider+":"+eventID], nil
}

func (s *stubProcessedTracker) MarkProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	key := provider + ":" + eventID
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	return true, nil
}

type stubOrgNumberResolver struct {
	from string
}

func (s *stubOrgNumberResolver) DefaultFromNumber(orgID string) string { return s.from }

type stubPaymentStatusStore struct{}

func (s *stubPaymentStatusStore) UpdateStatusByID(ctx context.Context, id uuid.UUID, status, providerRef string) (*paymentsql.Payment, error) {
	return nil, nil
}

func (s *stubPaymentStatusStore) GetByProviderRef(ctx context.Context, providerRef string) (*paymentsql.Payment, error) {
	return nil, nil
}

type stubOutboxWriter struct {
	inserted []struct {
		eventType string
		payload   any
	}
}

func (s *stubOutboxWriter) Insert(ctx context.Context, orgID string, eventType string, payload any) (uuid.UUID, error) {
	s.inserted = append(s.inserted, struct {
		eventType string
		payload   any
	}{eventType: eventType, payload: payload})
	return uuid.New(), nil
}

func buildSquarePayload(t *testing.T, eventID string, paymentID string, status string, metadata map[string]string) []byte {
	t.Helper()
	type payload struct {
		ID        string    `json:"id"`
		EventID   string    `json:"event_id"`
		CreatedAt time.Time `json:"created_at"`
		Type      string    `json:"type"`
		Data      struct {
			Object struct {
				Payment struct {
					ID          string `json:"id"`
					Status      string `json:"status"`
					OrderID     string `json:"order_id"`
					AmountMoney struct {
						Amount   int64  `json:"amount"`
						Currency string `json:"currency"`
					} `json:"amount_money"`
					Metadata map[string]string `json:"metadata"`
				} `json:"payment"`
			} `json:"object"`
		} `json:"data"`
	}
	var p payload
	p.ID = eventID
	p.EventID = eventID
	p.CreatedAt = time.Now().UTC().Truncate(time.Second)
	p.Type = "payment.updated"
	p.Data.Object.Payment.ID = paymentID
	p.Data.Object.Payment.Status = status
	p.Data.Object.Payment.AmountMoney.Amount = 5000
	p.Data.Object.Payment.AmountMoney.Currency = "USD"
	p.Data.Object.Payment.Metadata = metadata
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal square payload: %v", err)
	}
	return data
}

func signSquare(r *http.Request, key string, body []byte) {
	url := buildURLForSignature(r)
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(url + string(body)))
	r.Header.Set("X-Square-Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

func buildURLForSignature(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, r.URL.RequestURI())
}
