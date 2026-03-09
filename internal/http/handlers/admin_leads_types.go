package handlers

import (
	"database/sql"
	"strings"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminLeadsHandler handles admin API endpoints for lead management.
type AdminLeadsHandler struct {
	db     *sql.DB
	logger *logging.Logger
}

// NewAdminLeadsHandler creates a new admin leads handler.
func NewAdminLeadsHandler(db *sql.DB, logger *logging.Logger) *AdminLeadsHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &AdminLeadsHandler{
		db:     db,
		logger: logger,
	}
}

// LeadResponse represents a lead in API responses.
type LeadResponse struct {
	ID                   string   `json:"id"`
	OrgID                string   `json:"org_id"`
	Phone                string   `json:"phone"`
	Name                 string   `json:"name,omitempty"`
	Email                string   `json:"email,omitempty"`
	Status               string   `json:"status"`
	Source               string   `json:"source,omitempty"`
	InterestedServices   []string `json:"interested_services,omitempty"`
	LastContactAt        *string  `json:"last_contact_at,omitempty"`
	ConversationJobCount int      `json:"conversation_job_count"`
	PaymentTotal         int      `json:"payment_total_cents"`
	BookingCount         int      `json:"booking_count"`
	Tags                 []string `json:"tags,omitempty"`
	Notes                string   `json:"notes,omitempty"`
	CreatedAt            string   `json:"created_at"`
	UpdatedAt            string   `json:"updated_at"`
}

// LeadsListResponse represents a paginated list of leads.
type LeadsListResponse struct {
	Leads      []LeadResponse `json:"leads"`
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

// LeadDetailResponse represents detailed lead information.
type LeadDetailResponse struct {
	LeadResponse
	ConversationIDs []string         `json:"conversation_ids"`
	Payments        []PaymentSummary `json:"payments"`
	Bookings        []BookingSummary `json:"bookings"`
	Timeline        []TimelineEvent  `json:"timeline"`
}

// PaymentSummary represents a payment summary.
type PaymentSummary struct {
	ID          string `json:"id"`
	AmountCents int    `json:"amount_cents"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

// BookingSummary represents a booking summary.
type BookingSummary struct {
	ID          string `json:"id"`
	Service     string `json:"service"`
	ScheduledAt string `json:"scheduled_at"`
	Status      string `json:"status"`
}

// TimelineEvent represents an event in the lead timeline.
type TimelineEvent struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Timestamp   string `json:"timestamp"`
	Metadata    any    `json:"metadata,omitempty"`
}

// UpdateLeadRequest contains fields for updating a lead.
type UpdateLeadRequest struct {
	Name               *string  `json:"name,omitempty"`
	Email              *string  `json:"email,omitempty"`
	Status             *string  `json:"status,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	Notes              *string  `json:"notes,omitempty"`
	InterestedServices []string `json:"interested_services,omitempty"`
}

// LeadStatsResponse contains aggregated lead statistics.
type LeadStatsResponse struct {
	TotalLeads     int            `json:"total_leads"`
	ByStatus       map[string]int `json:"by_status"`
	NewThisWeek    int            `json:"new_this_week"`
	NewThisMonth   int            `json:"new_this_month"`
	ConversionRate float64        `json:"conversion_rate"`
}

// normalizePhoneDigits extracts just the digits from a phone number.
func normalizePhoneDigits(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	return digits.String()
}
