package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type paymentStatusStore interface {
	UpdateStatusByID(ctx context.Context, id uuid.UUID, status, providerRef string) (*paymentsql.Payment, error)
	GetByProviderRef(ctx context.Context, providerRef string) (*paymentsql.Payment, error)
}

type processedTracker interface {
	AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error)
	MarkProcessed(ctx context.Context, provider, eventID string) (bool, error)
}

type outboxWriter interface {
	Insert(ctx context.Context, orgID string, eventType string, payload any) (uuid.UUID, error)
}

type SquareWebhookHandler struct {
	signatureKey string
	payments     paymentStatusStore
	leads        leads.Repository
	processed    processedTracker
	outbox       outboxWriter
	numbers      OrgNumberResolver
	orders       orderMetadataFetcher
	logger       *logging.Logger
}

func NewSquareWebhookHandler(sigKey string, payments paymentStatusStore, leadsRepo leads.Repository, processed processedTracker, outbox outboxWriter, numbers OrgNumberResolver, orders orderMetadataFetcher, logger *logging.Logger) *SquareWebhookHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &SquareWebhookHandler{
		signatureKey: sigKey,
		payments:     payments,
		leads:        leadsRepo,
		processed:    processed,
		outbox:       outbox,
		numbers:      numbers,
		orders:       orders,
		logger:       logger,
	}
}

func (h *SquareWebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if !verifySquareSignature(h.signatureKey, buildAbsoluteURL(r), payload, r.Header.Get("X-Square-Signature")) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var evt squarePaymentEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		h.logger.Error("failed to decode square event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	eventID := evt.EventID
	if eventID == "" {
		eventID = evt.ID
	}
	if eventID == "" {
		http.Error(w, "missing event id", http.StatusBadRequest)
		return
	}

	if processed, err := h.processed.AlreadyProcessed(r.Context(), "square", eventID); err != nil {
		h.logger.Error("processed lookup failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	} else if processed {
		w.WriteHeader(http.StatusOK)
		return
	}

	metadata := evt.Data.Object.Payment.Metadata
	orgID := metadata["org_id"]
	leadID := metadata["lead_id"]
	intentID := metadata["booking_intent_id"]
	paymentID := evt.Data.Object.Payment.ID
	orderID := evt.Data.Object.Payment.OrderID
	scheduledStr := metadata["scheduled_for"]
	if orgID == "" || leadID == "" || intentID == "" {
		// Fallback: try to resolve via provider ref if metadata is missing (observed in some webhook payloads).
		if paymentID == "" {
			http.Error(w, "missing metadata", http.StatusBadRequest)
			return
		}
		var orderMeta map[string]string
		paymentRow, perr := h.payments.GetByProviderRef(r.Context(), paymentID)
		if perr != nil || paymentRow == nil {
			// If payment lookup fails, attempt to hydrate metadata via order lookup.
			if h.orders != nil && orderID != "" {
				if ometa, oerr := h.orders.FetchMetadata(r.Context(), orderID); oerr == nil && len(ometa) > 0 {
					orderMeta = ometa
					orgID = orderMeta["org_id"]
					leadID = orderMeta["lead_id"]
					intentID = orderMeta["booking_intent_id"]
					scheduledStr = orderMeta["scheduled_for"]
				} else {
					h.logger.Warn("square webhook missing metadata and payment/order lookup failed", "payment_err", perr, "order_err", oerr, "payment_id", paymentID, "order_id", orderID)
					w.WriteHeader(http.StatusOK) // acknowledge to avoid retries; cannot progress workflow
					return
				}
			} else {
				h.logger.Warn("square webhook missing metadata and payment lookup failed", "error", perr, "payment_id", paymentID)
				w.WriteHeader(http.StatusOK) // acknowledge to avoid retries; cannot progress workflow
				return
			}
		}
		if paymentRow != nil {
			orgID = paymentRow.OrgID
			if paymentRow.LeadID.Valid {
				leadUUID, _ := uuid.FromBytes(paymentRow.LeadID.Bytes[:])
				leadID = leadUUID.String()
			}
			if paymentRow.ID.Valid {
				intentID = uuid.UUID(paymentRow.ID.Bytes).String()
			}
		}
	}
	var scheduledFor *time.Time
	if scheduledStr != "" {
		if parsed, err := time.Parse(time.RFC3339, scheduledStr); err == nil {
			scheduledFor = &parsed
		} else {
			h.logger.Warn("square webhook scheduled_for parse failed", "error", err, "value", scheduledStr)
		}
	}
	if evt.Data.Object.Payment.Status != "COMPLETED" {
		w.WriteHeader(http.StatusOK)
		return
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

	if intentID == "" {
		http.Error(w, "missing booking intent id", http.StatusBadRequest)
		return
	}
	paymentUUID, err := uuid.Parse(intentID)
	if err != nil {
		http.Error(w, "invalid booking intent id", http.StatusBadRequest)
		return
	}

	providerRef := evt.Data.Object.Payment.ID
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

	event := events.PaymentSucceededV1{
		EventID:         eventID,
		OrgID:           orgID,
		LeadID:          leadID,
		BookingIntentID: paymentUUID.String(),
		Provider:        "square",
		ProviderRef:     providerRef,
		AmountCents:     evt.Data.Object.Payment.AmountMoney.Amount,
		OccurredAt:      evt.CreatedAt,
		LeadPhone:       lead.Phone,
		ScheduledFor:    scheduledFor,
	}
	if h.numbers != nil {
		event.FromNumber = h.numbers.DefaultFromNumber(orgID)
	}

	if _, err := h.outbox.Insert(r.Context(), orgID, "payment_succeeded.v1", event); err != nil {
		h.logger.Error("failed to enqueue outbox", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if _, err := h.processed.MarkProcessed(r.Context(), "square", eventID); err != nil {
		h.logger.Error("failed to record processed event", "error", err)
	}
	w.WriteHeader(http.StatusOK)
}

func verifySquareSignature(key, url string, body []byte, header string) bool {
	if key == "" || header == "" {
		return false
	}
	message := url + string(body)
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(message))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(header), []byte(expected))
}

type squarePaymentEvent struct {
	ID        string    `json:"id"`
	EventID   string    `json:"event_id"`
	CreatedAt time.Time `json:"created_at"`
	Type      string    `json:"type"`
	Data      struct {
		Object struct {
			Payment struct {
				ID          string            `json:"id"`
				Status      string            `json:"status"`
				OrderID     string            `json:"order_id"`
				AmountMoney squareMoney       `json:"amount_money"`
				Metadata    map[string]string `json:"metadata"`
			} `json:"payment"`
		} `json:"object"`
	} `json:"data"`
}

type squareMoney struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

func buildAbsoluteURL(r *http.Request) string {
	if r.URL == nil {
		return ""
	}
	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, r.URL.RequestURI())
}
