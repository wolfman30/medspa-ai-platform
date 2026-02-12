package payments

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// StripeWebhookHandler handles Stripe webhook events for checkout session completion.
type StripeWebhookHandler struct {
	webhookSecret string
	payments      paymentStatusStore
	leads         leads.Repository
	processed     processedTracker
	outbox        outboxWriter
	numbers       OrgNumberResolver
	logger        *logging.Logger
}

// NewStripeWebhookHandler creates a new handler for Stripe webhooks.
func NewStripeWebhookHandler(
	webhookSecret string,
	payments paymentStatusStore,
	leadsRepo leads.Repository,
	processed processedTracker,
	outbox outboxWriter,
	numbers OrgNumberResolver,
	logger *logging.Logger,
) *StripeWebhookHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &StripeWebhookHandler{
		webhookSecret: webhookSecret,
		payments:      payments,
		leads:         leadsRepo,
		processed:     processed,
		outbox:        outbox,
		numbers:       numbers,
		logger:        logger,
	}
}

// Handle processes incoming Stripe webhook events.
func (h *StripeWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	sigHeader := r.Header.Get("Stripe-Signature")
	if !verifyStripeSignature(h.webhookSecret, payload, sigHeader) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var evt stripeWebhookEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		h.logger.Error("failed to decode stripe event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if evt.ID == "" {
		http.Error(w, "missing event id", http.StatusBadRequest)
		return
	}

	// Only handle checkout.session.completed
	if evt.Type != "checkout.session.completed" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if processed, err := h.processed.AlreadyProcessed(r.Context(), "stripe", evt.ID); err != nil {
		h.logger.Error("processed lookup failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	} else if processed {
		w.WriteHeader(http.StatusOK)
		return
	}

	session := evt.Data.Object
	metadata := session.Metadata
	orgID := metadata["org_id"]
	leadID := metadata["lead_id"]
	intentID := metadata["booking_intent_id"]
	scheduledStr := metadata["scheduled_for"]
	fromNumber := metadata["from_number"]
	providerRef := session.PaymentIntent
	if providerRef == "" {
		providerRef = session.ID
	}

	if orgID == "" || leadID == "" || intentID == "" {
		h.logger.Warn("stripe webhook missing required metadata", "event_id", evt.ID, "metadata", metadata)
		// Acknowledge to prevent retries but can't progress workflow
		w.WriteHeader(http.StatusOK)
		return
	}

	var scheduledFor *time.Time
	if scheduledStr != "" {
		if parsed, err := time.Parse(time.RFC3339, scheduledStr); err == nil {
			scheduledFor = &parsed
		} else {
			h.logger.Warn("stripe webhook scheduled_for parse failed", "error", err, "value", scheduledStr)
		}
	}

	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		http.Error(w, "invalid org id", http.StatusBadRequest)
		return
	}
	orgID = orgUUID.String()

	leadUUID, err := uuid.Parse(leadID)
	if err != nil {
		http.Error(w, "invalid lead id", http.StatusBadRequest)
		return
	}
	leadID = leadUUID.String()

	paymentUUID, err := uuid.Parse(intentID)
	if err != nil {
		http.Error(w, "invalid booking intent id", http.StatusBadRequest)
		return
	}

	if _, err := h.payments.UpdateStatusByID(r.Context(), paymentUUID, "succeeded", providerRef); err != nil {
		h.logger.Error("failed to update payment record", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	lead, err := h.leads.GetByID(r.Context(), orgID, leadID)
	if err != nil {
		h.logger.Error("lead fetch failed", "error", err, "lead_id", leadID)
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}
	if err := h.leads.UpdateDepositStatus(r.Context(), leadID, "paid", "priority"); err != nil {
		h.logger.Warn("failed to update lead deposit status", "error", err, "lead_id", leadID, "org_id", orgID)
	}

	// Derive amount from session
	var amountCents int64
	if session.AmountTotal > 0 {
		amountCents = session.AmountTotal
	}

	event := events.PaymentSucceededV1{
		EventID:         evt.ID,
		OrgID:           orgID,
		LeadID:          leadID,
		BookingIntentID: paymentUUID.String(),
		Provider:        "stripe",
		ProviderRef:     providerRef,
		AmountCents:     amountCents,
		OccurredAt:      time.Unix(evt.Created, 0),
		LeadPhone:       lead.Phone,
		LeadName:        lead.Name,
		ScheduledFor:    scheduledFor,
		ServiceName:     leadService(lead),
	}
	if fromNumber == "" && h.numbers != nil {
		fromNumber = h.numbers.DefaultFromNumber(orgID)
	}
	event.FromNumber = fromNumber

	if _, err := h.outbox.Insert(r.Context(), orgID, "payment_succeeded.v1", event); err != nil {
		h.logger.Error("failed to enqueue outbox", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if _, err := h.processed.MarkProcessed(r.Context(), "stripe", evt.ID); err != nil {
		h.logger.Error("failed to record processed event", "error", err)
	}

	w.WriteHeader(http.StatusOK)
}

// stripeWebhookEvent represents a Stripe webhook event envelope.
type stripeWebhookEvent struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Created int64  `json:"created"`
	Data    struct {
		Object stripeSessionObject `json:"object"`
	} `json:"data"`
}

// stripeSessionObject is the checkout.session object from the webhook.
type stripeSessionObject struct {
	ID            string            `json:"id"`
	PaymentIntent string            `json:"payment_intent"`
	AmountTotal   int64             `json:"amount_total"`
	Currency      string            `json:"currency"`
	Metadata      map[string]string `json:"metadata"`
	Status        string            `json:"status"`
}

// verifyStripeSignature verifies a Stripe webhook signature.
// Stripe signs with HMAC-SHA256 and sends the signature in the Stripe-Signature header
// as: t=<timestamp>,v1=<signature>[,v0=<test_signature>]
func verifyStripeSignature(secret string, payload []byte, header string) bool {
	if secret == "" {
		return true // bypass for development
	}
	if header == "" {
		return false
	}

	var timestamp string
	var signatures []string

	parts := strings.Split(header, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}

	if timestamp == "" || len(signatures) == 0 {
		return false
	}

	// Check timestamp tolerance (5 minutes)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if abs64(time.Now().Unix()-ts) > 300 {
		return false
	}

	// Compute expected signature: HMAC-SHA256(secret, "timestamp.payload")
	signedPayload := fmt.Sprintf("%s.%s", timestamp, string(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	expected := hex.EncodeToString(mac.Sum(nil))

	for _, sig := range signatures {
		if hmac.Equal([]byte(sig), []byte(expected)) {
			return true
		}
	}
	return false
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func leadService(lead *leads.Lead) string {
	if lead == nil {
		return ""
	}
	if lead.SelectedService != "" {
		return lead.SelectedService
	}
	return lead.ServiceInterest
}
