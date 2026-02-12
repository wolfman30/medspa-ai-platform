package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func buildStripePayload(t *testing.T, eventID, eventType, sessionID, paymentIntentID string, amountTotal int64, metadata map[string]string) []byte {
	t.Helper()
	evt := map[string]any{
		"id":      eventID,
		"type":    eventType,
		"created": time.Now().Unix(),
		"data": map[string]any{
			"object": map[string]any{
				"id":             sessionID,
				"payment_intent": paymentIntentID,
				"amount_total":   amountTotal,
				"currency":       "usd",
				"metadata":       metadata,
				"status":         "complete",
			},
		},
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("failed to marshal stripe event: %v", err)
	}
	return data
}

func stripeSign(payload []byte, secret string) string {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(payload)))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%s,v1=%s", ts, sig)
}

func TestStripeWebhookHandler_Success(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()
	scheduled := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	payments := &stubPaymentStore{}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000", Name: "Jane Doe"},
	}
	processed := &stubProcessedTracker{}
	outbox := &stubOutboxWriter{}
	numbers := stubNumberResolver("+19998887777")

	handler := NewStripeWebhookHandler("whsec_test123", payments, leadsRepo, processed, outbox, numbers, logging.Default())

	body := buildStripePayload(t, "evt_stripe_123", "checkout.session.completed", "cs_123", "pi_123", 5000, map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
		"scheduled_for":     scheduled.Format(time.RFC3339),
		"from_number":       "+15551234567",
	})

	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/stripe", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", stripeSign(body, "whsec_test123"))

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !payments.called {
		t.Fatal("expected payment update")
	}
	if len(outbox.inserted) != 1 {
		t.Fatalf("expected 1 outbox insert, got %d", len(outbox.inserted))
	}

	evt := outbox.inserted[0]
	if evt.Provider != "stripe" {
		t.Fatalf("expected provider stripe, got %s", evt.Provider)
	}
	if evt.ProviderRef != "pi_123" {
		t.Fatalf("expected provider ref pi_123, got %s", evt.ProviderRef)
	}
	if evt.AmountCents != 5000 {
		t.Fatalf("expected 5000 cents, got %d", evt.AmountCents)
	}
	if evt.FromNumber != "+15551234567" {
		t.Fatalf("expected from_number from metadata, got %s", evt.FromNumber)
	}
	if evt.ScheduledFor == nil || !evt.ScheduledFor.Equal(scheduled) {
		t.Fatalf("expected scheduled_for to propagate, got %v", evt.ScheduledFor)
	}
	if evt.LeadPhone != "+15550000000" {
		t.Fatalf("expected lead phone, got %s", evt.LeadPhone)
	}
	if !processed.marked {
		t.Fatal("expected processed marker")
	}
}

func TestStripeWebhookHandler_InvalidSignature(t *testing.T) {
	handler := NewStripeWebhookHandler("whsec_test123", nil, nil, nil, nil, nil, logging.Default())

	body := []byte(`{"id":"evt_1","type":"checkout.session.completed"}`)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/stripe", bytes.NewReader(body))
	req.Header.Set("Stripe-Signature", "t=12345,v1=bad_signature")

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestStripeWebhookHandler_IgnoresNonCheckoutEvents(t *testing.T) {
	handler := NewStripeWebhookHandler("", nil, nil, &stubProcessedTracker{}, nil, nil, logging.Default())

	body := buildStripePayload(t, "evt_other", "payment_intent.succeeded", "pi_123", "", 5000, nil)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/stripe", bytes.NewReader(body))

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for ignored event, got %d", rr.Code)
	}
}

func TestStripeWebhookHandler_DuplicateEvent(t *testing.T) {
	processed := &stubProcessedTracker{already: true}
	handler := NewStripeWebhookHandler("", nil, nil, processed, nil, nil, logging.Default())

	body := buildStripePayload(t, "evt_dup", "checkout.session.completed", "cs_dup", "pi_dup", 5000, map[string]string{
		"org_id":            uuid.New().String(),
		"lead_id":           uuid.New().String(),
		"booking_intent_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/stripe", bytes.NewReader(body))

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for duplicate, got %d", rr.Code)
	}
}

func TestStripeWebhookHandler_MissingMetadata(t *testing.T) {
	processed := &stubProcessedTracker{}
	handler := NewStripeWebhookHandler("", nil, nil, processed, nil, nil, logging.Default())

	body := buildStripePayload(t, "evt_missing", "checkout.session.completed", "cs_miss", "pi_miss", 5000, map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/stripe", bytes.NewReader(body))

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	// Should acknowledge (200) but not process
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for missing metadata, got %d", rr.Code)
	}
}

func TestStripeWebhookHandler_FromNumberFallback(t *testing.T) {
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

	handler := NewStripeWebhookHandler("", payments, leadsRepo, processed, outbox, numbers, logging.Default())

	// No from_number in metadata — should fall back to number resolver
	body := buildStripePayload(t, "evt_fallback", "checkout.session.completed", "cs_fb", "pi_fb", 5000, map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
	})
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/stripe", bytes.NewReader(body))

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(outbox.inserted) != 1 {
		t.Fatalf("expected 1 outbox insert, got %d", len(outbox.inserted))
	}
	if outbox.inserted[0].FromNumber != "+19998887777" {
		t.Fatalf("expected fallback from number, got %s", outbox.inserted[0].FromNumber)
	}
}

func TestStripeWebhookHandler_PaymentSucceededEventType(t *testing.T) {
	// Verify that the outbox event type is "payment_succeeded.v1" — same as Square
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()

	payments := &stubPaymentStore{}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000"},
	}
	processed := &stubProcessedTracker{}
	outbox := &recordingOutboxWriter{}

	handler := NewStripeWebhookHandler("", payments, leadsRepo, processed, outbox, nil, logging.Default())

	body := buildStripePayload(t, "evt_type_check", "checkout.session.completed", "cs_tc", "pi_tc", 7500, map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
	})
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/stripe", bytes.NewReader(body))

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(outbox.entries) != 1 {
		t.Fatalf("expected 1 outbox entry, got %d", len(outbox.entries))
	}
	if outbox.entries[0].eventType != "payment_succeeded.v1" {
		t.Fatalf("expected event type payment_succeeded.v1, got %s", outbox.entries[0].eventType)
	}
}

// Signature verification unit tests

func TestVerifyStripeSignature_Valid(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_1"}`)
	sig := stripeSign(payload, secret)

	if !verifyStripeSignature(secret, payload, sig) {
		t.Fatal("expected valid signature to pass")
	}
}

func TestVerifyStripeSignature_Invalid(t *testing.T) {
	if verifyStripeSignature("secret", []byte("payload"), "t=123,v1=bad") {
		t.Fatal("expected invalid signature to fail")
	}
}

func TestVerifyStripeSignature_EmptySecret(t *testing.T) {
	// Empty secret bypasses verification (dev mode)
	if !verifyStripeSignature("", []byte("any"), "any") {
		t.Fatal("expected empty secret to bypass")
	}
}

func TestVerifyStripeSignature_EmptyHeader(t *testing.T) {
	if verifyStripeSignature("secret", []byte("payload"), "") {
		t.Fatal("expected empty header to fail")
	}
}

// recordingOutboxWriter records the event type alongside the payload.
type recordingOutboxWriter struct {
	entries []outboxEntry
}

type outboxEntry struct {
	orgID     string
	eventType string
	payload   any
}

func (w *recordingOutboxWriter) Insert(ctx context.Context, orgID string, eventType string, payload any) (uuid.UUID, error) {
	w.entries = append(w.entries, outboxEntry{orgID: orgID, eventType: eventType, payload: payload})
	// Also satisfy the stubOutboxWriter interface for PaymentSucceededV1
	return uuid.New(), nil
}

// Ensure the emitted event is compatible with the events.PaymentSucceededV1 struct
func TestStripeWebhookEmitsCorrectEventStruct(t *testing.T) {
	orgID := uuid.New().String()
	leadID := uuid.New().String()
	intentID := uuid.New().String()

	payments := &stubPaymentStore{}
	leadsRepo := &stubLeadRepo{
		lead: &leads.Lead{ID: leadID, OrgID: orgID, Phone: "+15550000000", Name: "Test Patient"},
	}
	processed := &stubProcessedTracker{}
	outbox := &stubOutboxWriter{}

	handler := NewStripeWebhookHandler("", payments, leadsRepo, processed, outbox, nil, logging.Default())

	body := buildStripePayload(t, "evt_struct", "checkout.session.completed", "cs_s", "pi_s", 10000, map[string]string{
		"org_id":            orgID,
		"lead_id":           leadID,
		"booking_intent_id": intentID,
	})
	req := httptest.NewRequest(http.MethodPost, "https://example.com/webhooks/stripe", bytes.NewReader(body))

	rr := httptest.NewRecorder()
	handler.Handle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(outbox.inserted) != 1 {
		t.Fatalf("expected 1 event, got %d", len(outbox.inserted))
	}

	evt := outbox.inserted[0]
	// Verify it's a proper PaymentSucceededV1
	var _ events.PaymentSucceededV1 = evt
	if evt.OrgID != orgID {
		t.Errorf("org_id mismatch")
	}
	if evt.LeadID != leadID {
		t.Errorf("lead_id mismatch")
	}
	if evt.LeadName != "Test Patient" {
		t.Errorf("lead_name mismatch: got %s", evt.LeadName)
	}
}
