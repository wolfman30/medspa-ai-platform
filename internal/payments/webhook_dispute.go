package payments

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// DisputeWebhookHandler handles Square dispute webhook events.
type DisputeWebhookHandler struct {
	db              *sql.DB
	logger          *logging.Logger
	notifyService   DisputeNotifier
	evidenceService EvidenceCollector
}

// DisputeNotifier sends notifications about disputes.
type DisputeNotifier interface {
	NotifyDispute(ctx context.Context, dispute *DisputeEvent) error
}

// EvidenceCollector gathers evidence for dispute defense.
type EvidenceCollector interface {
	CollectEvidence(ctx context.Context, paymentID string) (*DisputeEvidence, error)
}

// DisputeEvent represents a Square dispute event.
type DisputeEvent struct {
	ID            string
	PaymentID     string
	DisputeID     string
	State         DisputeState
	Reason        DisputeReason
	AmountCents   int32
	Currency      string
	DueAt         time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CardBrand     string
	CustomerPhone string
	CustomerEmail string
	OrgID         string
}

// DisputeState represents the state of a dispute.
type DisputeState string

const (
	DisputeStateInquiryEvidenceRequired DisputeState = "INQUIRY_EVIDENCE_REQUIRED"
	DisputeStateWaitingThirdParty       DisputeState = "WAITING_THIRD_PARTY"
	DisputeStateEvidenceRequired        DisputeState = "EVIDENCE_REQUIRED"
	DisputeStateProcessing              DisputeState = "PROCESSING"
	DisputeStateWon                     DisputeState = "WON"
	DisputeStateLost                    DisputeState = "LOST"
	DisputeStateAccepted                DisputeState = "ACCEPTED"
)

// DisputeReason represents why the dispute was filed.
type DisputeReason string

const (
	DisputeReasonAmountDiffers    DisputeReason = "AMOUNT_DIFFERS"
	DisputeReasonCanceled         DisputeReason = "CANCELED"
	DisputeReasonDuplicate        DisputeReason = "DUPLICATE"
	DisputeReasonNoKnowledge      DisputeReason = "NO_KNOWLEDGE"
	DisputeReasonNotAsDescribed   DisputeReason = "NOT_AS_DESCRIBED"
	DisputeReasonNotReceived      DisputeReason = "NOT_RECEIVED"
	DisputeReasonPaidOther        DisputeReason = "PAID_BY_OTHER_MEANS"
	DisputeReasonCustomerRequest  DisputeReason = "CUSTOMER_REQUESTS_CREDIT"
	DisputeReasonEMVLiabilityShft DisputeReason = "EMV_LIABILITY_SHIFT"
)

// DisputeEvidence contains evidence for dispute defense.
type DisputeEvidence struct {
	PaymentID              string
	ConversationTranscript string
	PaymentConfirmation    string
	CustomerConsent        string
	ServiceDescription     string
	AdditionalNotes        string
	CollectedAt            time.Time
}

// NewDisputeWebhookHandler creates a new dispute webhook handler.
func NewDisputeWebhookHandler(db *sql.DB, notifier DisputeNotifier, evidenceCollector EvidenceCollector, logger *logging.Logger) *DisputeWebhookHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &DisputeWebhookHandler{
		db:              db,
		logger:          logger,
		notifyService:   notifier,
		evidenceService: evidenceCollector,
	}
}

// HandleWebhook processes a Square dispute webhook event.
func (h *DisputeWebhookHandler) HandleWebhook(ctx context.Context, eventType string, payload []byte) error {
	ctx, span := squareTracer.Start(ctx, "square.dispute_webhook")
	defer span.End()
	span.SetAttributes(attribute.String("square.event_type", eventType))

	switch eventType {
	case "dispute.created":
		return h.handleDisputeCreated(ctx, payload)
	case "dispute.state.changed", "dispute.state.updated":
		return h.handleDisputeStateChanged(ctx, payload)
	case "dispute.evidence.created":
		return h.handleEvidenceCreated(ctx, payload)
	default:
		h.logger.Debug("ignoring dispute event", "type", eventType)
		return nil
	}
}

// handleDisputeCreated handles a new dispute.
func (h *DisputeWebhookHandler) handleDisputeCreated(ctx context.Context, payload []byte) error {
	var event struct {
		Data struct {
			Object struct {
				Dispute squareDispute `json:"dispute"`
			} `json:"object"`
		} `json:"data"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("payments: dispute unmarshal: %w", err)
	}

	dispute := h.mapSquareDispute(&event.Data.Object.Dispute)

	h.logger.Warn("dispute received",
		"dispute_id", dispute.DisputeID,
		"payment_id", dispute.PaymentID,
		"reason", dispute.Reason,
		"amount_cents", dispute.AmountCents,
		"due_at", dispute.DueAt,
	)

	// Store dispute in database
	if err := h.storeDispute(ctx, dispute); err != nil {
		h.logger.Error("failed to store dispute", "error", err)
		// Continue to notification even if storage fails
	}

	// Notify staff immediately
	if h.notifyService != nil {
		if err := h.notifyService.NotifyDispute(ctx, dispute); err != nil {
			h.logger.Error("failed to notify staff of dispute", "error", err)
		}
	}

	// Start evidence collection
	if h.evidenceService != nil {
		go func() {
			bgCtx := context.Background()
			evidence, err := h.evidenceService.CollectEvidence(bgCtx, dispute.PaymentID)
			if err != nil {
				h.logger.Error("failed to collect dispute evidence", "error", err)
				return
			}
			if err := h.storeEvidence(bgCtx, dispute.DisputeID, evidence); err != nil {
				h.logger.Error("failed to store dispute evidence", "error", err)
			}
		}()
	}

	return nil
}

// handleDisputeStateChanged handles dispute state updates.
func (h *DisputeWebhookHandler) handleDisputeStateChanged(ctx context.Context, payload []byte) error {
	var event struct {
		Data struct {
			Object struct {
				Dispute squareDispute `json:"dispute"`
			} `json:"object"`
		} `json:"data"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return fmt.Errorf("payments: dispute state unmarshal: %w", err)
	}

	dispute := h.mapSquareDispute(&event.Data.Object.Dispute)

	h.logger.Info("dispute state changed",
		"dispute_id", dispute.DisputeID,
		"new_state", dispute.State,
		"payment_id", dispute.PaymentID,
	)

	// Update dispute in database
	if err := h.updateDisputeState(ctx, dispute.DisputeID, dispute.State); err != nil {
		h.logger.Error("failed to update dispute state", "error", err)
	}

	// Notify on resolution
	if dispute.State == DisputeStateWon || dispute.State == DisputeStateLost || dispute.State == DisputeStateAccepted {
		if h.notifyService != nil {
			if err := h.notifyService.NotifyDispute(ctx, dispute); err != nil {
				h.logger.Error("failed to notify staff of dispute resolution", "error", err)
			}
		}
	}

	return nil
}

// handleEvidenceCreated handles evidence submission confirmation.
func (h *DisputeWebhookHandler) handleEvidenceCreated(ctx context.Context, payload []byte) error {
	h.logger.Info("dispute evidence submitted successfully")
	return nil
}

// squareDispute represents the Square API dispute structure.
type squareDispute struct {
	ID                string        `json:"id"`
	DisputeID         string        `json:"dispute_id"`
	DisputedPaymentID string        `json:"disputed_payment_id"`
	PaymentID         string        `json:"payment_id"`
	State             string        `json:"state"`
	Reason            string        `json:"reason"`
	AmountMoney       *squareAmount `json:"amount_money"`
	DueAt             string        `json:"due_at"`
	CreatedAt         string        `json:"created_at"`
	UpdatedAt         string        `json:"updated_at"`
	CardBrand         string        `json:"card_brand"`
	LocationID        string        `json:"location_id"`
}

type squareAmount struct {
	Amount   int32  `json:"amount"`
	Currency string `json:"currency"`
}

func (h *DisputeWebhookHandler) mapSquareDispute(sd *squareDispute) *DisputeEvent {
	dispute := &DisputeEvent{
		ID:        sd.ID,
		DisputeID: sd.DisputeID,
		PaymentID: sd.DisputedPaymentID,
		State:     DisputeState(sd.State),
		Reason:    DisputeReason(sd.Reason),
		CardBrand: sd.CardBrand,
	}

	// Handle alternate payment ID field
	if dispute.PaymentID == "" {
		dispute.PaymentID = sd.PaymentID
	}

	if sd.AmountMoney != nil {
		dispute.AmountCents = sd.AmountMoney.Amount
		dispute.Currency = sd.AmountMoney.Currency
	}

	if sd.DueAt != "" {
		dispute.DueAt, _ = time.Parse(time.RFC3339, sd.DueAt)
	}
	if sd.CreatedAt != "" {
		dispute.CreatedAt, _ = time.Parse(time.RFC3339, sd.CreatedAt)
	}
	if sd.UpdatedAt != "" {
		dispute.UpdatedAt, _ = time.Parse(time.RFC3339, sd.UpdatedAt)
	}

	return dispute
}

func (h *DisputeWebhookHandler) storeDispute(ctx context.Context, dispute *DisputeEvent) error {
	// Lookup org_id from the original payment
	var orgID sql.NullString
	err := h.db.QueryRowContext(ctx,
		`SELECT org_id FROM payments WHERE provider_ref = $1`,
		dispute.PaymentID,
	).Scan(&orgID)
	if err != nil && err != sql.ErrNoRows {
		h.logger.Warn("failed to lookup org_id for dispute", "payment_id", dispute.PaymentID, "error", err)
	}

	query := `
		INSERT INTO payment_disputes (
			id, dispute_id, payment_id, org_id, state, reason, amount_cents, currency,
			due_at, card_brand, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (dispute_id) DO UPDATE SET
			state = EXCLUDED.state,
			updated_at = EXCLUDED.updated_at
	`

	_, err = h.db.ExecContext(ctx, query,
		uuid.New(),
		dispute.DisputeID,
		dispute.PaymentID,
		orgID,
		string(dispute.State),
		string(dispute.Reason),
		dispute.AmountCents,
		dispute.Currency,
		dispute.DueAt,
		dispute.CardBrand,
		dispute.CreatedAt,
		time.Now(),
	)
	return err
}

func (h *DisputeWebhookHandler) updateDisputeState(ctx context.Context, disputeID string, state DisputeState) error {
	query := `UPDATE payment_disputes SET state = $1, updated_at = $2 WHERE dispute_id = $3`
	_, err := h.db.ExecContext(ctx, query, string(state), time.Now(), disputeID)
	return err
}

func (h *DisputeWebhookHandler) storeEvidence(ctx context.Context, disputeID string, evidence *DisputeEvidence) error {
	query := `
		INSERT INTO dispute_evidence (
			id, dispute_id, conversation_transcript, payment_confirmation,
			customer_consent, service_description, additional_notes, collected_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := h.db.ExecContext(ctx, query,
		uuid.New(),
		disputeID,
		evidence.ConversationTranscript,
		evidence.PaymentConfirmation,
		evidence.CustomerConsent,
		evidence.ServiceDescription,
		evidence.AdditionalNotes,
		evidence.CollectedAt,
	)
	return err
}

// RegisterDisputeRoutes registers the dispute webhook routes.
func (h *DisputeWebhookHandler) RegisterRoutes(mux *http.ServeMux) {
	// Disputes are handled through the main Square webhook endpoint
	// This is a convenience method if separate routing is needed
	mux.HandleFunc("/webhooks/square/disputes", h.handleHTTP)
}

func (h *DisputeWebhookHandler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body once to avoid consumption issues
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Parse to extract event type
	var envelope struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// Handle the event with full body bytes
	if err := h.HandleWebhook(r.Context(), envelope.Type, bodyBytes); err != nil {
		h.logger.Error("dispute webhook error", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
