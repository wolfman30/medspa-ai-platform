package payments

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/tenancy"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// OrgNumberResolver exposes the preferred Twilio number for an org.
type OrgNumberResolver interface {
	DefaultFromNumber(orgID string) string
}

type CheckoutHandler struct {
	leads     leads.Repository
	payments  *Repository
	square    *SquareCheckoutService
	logger    *logging.Logger
	minAmount int32
}

type checkoutRequest struct {
	LeadID          string `json:"lead_id"`
	AmountCents     int32  `json:"amount_cents"`
	BookingIntentID string `json:"booking_intent_id,omitempty"`
	SuccessURL      string `json:"success_url,omitempty"`
	CancelURL       string `json:"cancel_url,omitempty"`
	ScheduledFor    string `json:"scheduled_for,omitempty"`
}

type checkoutResponse struct {
	CheckoutURL string `json:"checkout_url"`
	Provider    string `json:"provider"`
}

func NewCheckoutHandler(leadsRepo leads.Repository, paymentsRepo *Repository, square *SquareCheckoutService, logger *logging.Logger, minAmount int32) *CheckoutHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &CheckoutHandler{
		leads:     leadsRepo,
		payments:  paymentsRepo,
		square:    square,
		logger:    logger,
		minAmount: minAmount,
	}
}

func (h *CheckoutHandler) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	orgID, ok := tenancy.OrgIDFromContext(r.Context())
	if !ok {
		http.Error(w, "missing org context", http.StatusBadRequest)
		return
	}

	var req checkoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if req.LeadID == "" {
		http.Error(w, "lead_id is required", http.StatusBadRequest)
		return
	}
	if req.AmountCents <= 0 {
		req.AmountCents = h.minAmount
	}
	var scheduledFor *time.Time
	if req.ScheduledFor != "" {
		parsed, err := time.Parse(time.RFC3339, req.ScheduledFor)
		if err != nil {
			http.Error(w, "invalid scheduled_for format", http.StatusBadRequest)
			return
		}
		scheduledFor = &parsed
	}

	lead, err := h.leads.GetByID(r.Context(), orgID, req.LeadID)
	if err != nil {
		h.logger.Error("lead lookup failed", "error", err, "org_id", orgID, "lead_id", req.LeadID)
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}

	leadUUID, err := uuid.Parse(req.LeadID)
	if err != nil {
		http.Error(w, "invalid lead_id format", http.StatusBadRequest)
		return
	}
	orgUUID, err := uuid.Parse(orgID)
	if err != nil {
		http.Error(w, "invalid org id", http.StatusBadRequest)
		return
	}
	intent, err := h.payments.CreateIntent(r.Context(), orgUUID, leadUUID, "square", uuid.Nil, req.AmountCents, "deposit_pending", scheduledFor)
	if err != nil {
		h.logger.Error("failed to persist payment intent", "error", err)
		http.Error(w, "failed to create payment intent", http.StatusInternalServerError)
		return
	}
	var paymentID uuid.UUID
	if intent.ID.Valid {
		paymentID = uuid.UUID(intent.ID.Bytes)
	} else {
		paymentID = uuid.New()
	}

	link, err := h.square.CreatePaymentLink(r.Context(), CheckoutParams{
		OrgID:           orgID,
		LeadID:          req.LeadID,
		AmountCents:     req.AmountCents,
		BookingIntentID: paymentID,
		Description:     "MedSpa Deposit",
		SuccessURL:      req.SuccessURL,
		CancelURL:       req.CancelURL,
		ScheduledFor:    scheduledFor,
	})
	if err != nil {
		h.logger.Error("square checkout failed", "error", err)
		http.Error(w, "failed to create checkout session", http.StatusBadGateway)
		return
	}

	resp := checkoutResponse{
		CheckoutURL: link.URL,
		Provider:    "square",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	h.logger.Info("deposit checkout created", "org_id", orgID, "lead_id", lead.ID, "url", link.URL)
}
