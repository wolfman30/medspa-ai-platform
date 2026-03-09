package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GetLead returns detailed information about a specific lead.
// GET /admin/orgs/{orgID}/leads/{leadID}
func (h *AdminLeadsHandler) GetLead(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	leadID := chi.URLParam(r, "leadID")

	if orgID == "" || leadID == "" {
		http.Error(w, "missing orgID or leadID", http.StatusBadRequest)
		return
	}

	leadUUID, err := uuid.Parse(leadID)
	if err != nil {
		http.Error(w, "invalid leadID", http.StatusBadRequest)
		return
	}

	// Get lead details
	query := `
		SELECT id, org_id, phone, name, email, status, source,
			   interested_services, tags, notes, created_at, updated_at
		FROM leads
		WHERE id = $1 AND org_id = $2
	`
	var lead LeadDetailResponse
	var name, email, source, notes sql.NullString
	var interestedServices, tags []byte
	var createdAt, updatedAt time.Time

	err = h.db.QueryRowContext(r.Context(), query, leadUUID, orgID).Scan(
		&lead.ID, &lead.OrgID, &lead.Phone, &name, &email, &lead.Status, &source,
		&interestedServices, &tags, &notes, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}
	if err != nil {
		h.logger.Error("failed to get lead", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	lead.Name = name.String
	lead.Email = email.String
	lead.Source = source.String
	lead.Notes = notes.String
	lead.CreatedAt = createdAt.Format(time.RFC3339)
	lead.UpdatedAt = updatedAt.Format(time.RFC3339)
	if len(interestedServices) > 0 {
		if err := json.Unmarshal(interestedServices, &lead.InterestedServices); err != nil {
			h.logger.Error("failed to decode interested_services", "lead_id", lead.ID, "error", err)
		}
	}
	if len(tags) > 0 {
		if err := json.Unmarshal(tags, &lead.Tags); err != nil {
			h.logger.Error("failed to decode tags", "lead_id", lead.ID, "error", err)
		}
	}

	// Get distinct conversation IDs from conversation_jobs via phone
	normalizedPhone := normalizePhoneDigits(lead.Phone)
	conversationPattern := "sms:" + orgID + ":%" + normalizedPhone + "%"

	convIDRows, err := h.db.QueryContext(r.Context(),
		`SELECT DISTINCT conversation_id FROM conversation_jobs WHERE conversation_id LIKE $1 ORDER BY conversation_id`,
		conversationPattern,
	)
	if err != nil {
		h.logger.Error("failed to get conversation ids", "lead_id", lead.ID, "error", err)
	} else {
		defer convIDRows.Close()
		for convIDRows.Next() {
			var convID string
			if err := convIDRows.Scan(&convID); err != nil {
				h.logger.Error("failed to scan conversation id", "lead_id", lead.ID, "error", err)
				continue
			}
			lead.ConversationIDs = append(lead.ConversationIDs, convID)
		}
		if err := convIDRows.Err(); err != nil {
			h.logger.Error("conversation id rows iteration failed", "lead_id", lead.ID, "error", err)
		}
	}

	// Get job count
	var jobCount int
	if err := h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM conversation_jobs WHERE conversation_id LIKE $1`,
		conversationPattern,
	).Scan(&jobCount); err != nil {
		h.logger.Error("failed to get conversation job count", "lead_id", lead.ID, "error", err)
	} else {
		lead.ConversationJobCount = jobCount
	}

	// Get payments
	payQuery := `
		SELECT id, amount_cents, status, created_at
		FROM payments
		WHERE lead_id = $1
		ORDER BY created_at DESC
		LIMIT 10
	`
	payRows, err := h.db.QueryContext(r.Context(), payQuery, leadUUID)
	if err != nil {
		h.logger.Error("failed to query payments", "lead_id", lead.ID, "error", err)
	} else {
		defer payRows.Close()
		for payRows.Next() {
			var pay PaymentSummary
			var payCreatedAt time.Time
			err := payRows.Scan(&pay.ID, &pay.AmountCents, &pay.Status, &payCreatedAt)
			if err != nil {
				h.logger.Error("failed to scan payment", "lead_id", lead.ID, "error", err)
				continue
			}
			pay.CreatedAt = payCreatedAt.Format(time.RFC3339)
			lead.Payments = append(lead.Payments, pay)
		}
		if err := payRows.Err(); err != nil {
			h.logger.Error("payment rows iteration failed", "lead_id", lead.ID, "error", err)
		}
	}

	// Get bookings
	bookQuery := `
		SELECT id, service_name, scheduled_at, status
		FROM bookings
		WHERE lead_id = $1
		ORDER BY scheduled_at DESC
		LIMIT 10
	`
	bookRows, err := h.db.QueryContext(r.Context(), bookQuery, leadUUID)
	if err != nil {
		h.logger.Error("failed to query bookings", "lead_id", lead.ID, "error", err)
	} else {
		defer bookRows.Close()
		for bookRows.Next() {
			var book BookingSummary
			var scheduledAt time.Time
			err := bookRows.Scan(&book.ID, &book.Service, &scheduledAt, &book.Status)
			if err != nil {
				h.logger.Error("failed to scan booking", "lead_id", lead.ID, "error", err)
				continue
			}
			book.ScheduledAt = scheduledAt.Format(time.RFC3339)
			lead.Bookings = append(lead.Bookings, book)
		}
		if err := bookRows.Err(); err != nil {
			h.logger.Error("booking rows iteration failed", "lead_id", lead.ID, "error", err)
		}
	}

	// Initialize empty arrays if nil
	if lead.ConversationIDs == nil {
		lead.ConversationIDs = []string{}
	}
	if lead.Payments == nil {
		lead.Payments = []PaymentSummary{}
	}
	if lead.Bookings == nil {
		lead.Bookings = []BookingSummary{}
	}
	if lead.Timeline == nil {
		lead.Timeline = []TimelineEvent{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lead)
}
