package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BookingCallbackHandler handles callbacks about booking outcomes
// (success, payment failure, timeout, etc.).
type BookingCallbackHandler struct {
	leadsRepo leads.Repository
	messenger ReplyMessenger
	logger    *logging.Logger
}

// NewBookingCallbackHandler creates a new BookingCallbackHandler.
func NewBookingCallbackHandler(repo leads.Repository, messenger ReplyMessenger, logger *logging.Logger) *BookingCallbackHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &BookingCallbackHandler{
		leadsRepo: repo,
		messenger: messenger,
		logger:    logger,
	}
}

// bookingCallbackPayload is the JSON body POSTed by the booking callback.
type bookingCallbackPayload struct {
	SessionID           string                       `json:"sessionId"`
	State               string                       `json:"state"`
	Outcome             string                       `json:"outcome"`
	ConfirmationDetails *bookingCallbackConfirmation `json:"confirmationDetails,omitempty"`
	Error               string                       `json:"error,omitempty"`
}

type bookingCallbackConfirmation struct {
	ConfirmationNumber string `json:"confirmationNumber,omitempty"`
	AppointmentTime    string `json:"appointmentTime,omitempty"`
	Provider           string `json:"provider,omitempty"`
	Service            string `json:"service,omitempty"`
}

// Handle processes POST /webhooks/booking/callback?orgId=X&from=Y.
func (h *BookingCallbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("orgId")
	fromNumber := r.URL.Query().Get("from")

	var payload bookingCallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if payload.SessionID == "" {
		http.Error(w, "sessionId is required", http.StatusBadRequest)
		return
	}

	h.logger.Info("booking callback received",
		"session_id", payload.SessionID,
		"state", payload.State,
		"outcome", payload.Outcome,
		"org_id", orgID,
	)

	// Look up lead by session ID
	lead, err := h.leadsRepo.GetByBookingSessionID(r.Context(), payload.SessionID)
	if err != nil {
		if errors.Is(err, leads.ErrLeadNotFound) {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		h.logger.Error("failed to look up lead by booking session", "error", err, "session_id", payload.SessionID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Update lead with outcome
	now := time.Now()
	update := leads.BookingSessionUpdate{
		Outcome:     payload.Outcome,
		CompletedAt: &now,
	}
	if payload.ConfirmationDetails != nil {
		update.ConfirmationNumber = payload.ConfirmationDetails.ConfirmationNumber
	}
	if err := h.leadsRepo.UpdateBookingSession(r.Context(), lead.ID, update); err != nil {
		h.logger.Error("failed to update lead booking outcome", "error", err, "lead_id", lead.ID, "outcome", payload.Outcome)
	}

	// Build and send outcome SMS
	smsBody := bookingOutcomeSMS(payload.Outcome, payload.ConfirmationDetails)
	if smsBody != "" && h.messenger != nil && lead.Phone != "" && fromNumber != "" {
		reply := OutboundReply{
			OrgID:  orgID,
			LeadID: lead.ID,
			To:     lead.Phone,
			From:   fromNumber,
			Body:   smsBody,
		}
		if err := h.messenger.SendReply(r.Context(), reply); err != nil {
			h.logger.Error("failed to send booking outcome SMS", "error", err,
				"lead_id", lead.ID, "outcome", payload.Outcome)
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// bookingOutcomeSMS returns the SMS body for a given booking outcome.
func bookingOutcomeSMS(outcome string, details *bookingCallbackConfirmation) string {
	switch outcome {
	case "success":
		msg := "Your appointment is confirmed!"
		if details != nil && details.ConfirmationNumber != "" {
			msg += fmt.Sprintf(" Confirmation #%s.", details.ConfirmationNumber)
		}
		if details != nil && details.AppointmentTime != "" {
			msg += fmt.Sprintf(" Scheduled for %s.", details.AppointmentTime)
		}
		msg += " We look forward to seeing you!"
		return msg
	case "payment_failed":
		return "Your payment didn't go through. Reply YES to try again, or call the clinic directly for help."
	case "slot_unavailable":
		return "That time slot is no longer available. Want me to check other times for you?"
	case "timeout":
		return "Your booking session expired. Would you like to try again?"
	case "error":
		return "Something went wrong with your booking. Please try again or call the clinic directly."
	default:
		return ""
	}
}
