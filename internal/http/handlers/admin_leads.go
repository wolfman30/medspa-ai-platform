package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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

// ListLeads returns a paginated list of leads for an organization.
// GET /admin/orgs/{orgID}/leads
func (h *AdminLeadsHandler) ListLeads(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing orgID", http.StatusBadRequest)
		return
	}

	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")
	sortBy := r.URL.Query().Get("sort_by")
	if sortBy == "" {
		sortBy = "created_at"
	}
	sortOrder := r.URL.Query().Get("sort_order")
	if sortOrder != "asc" {
		sortOrder = "desc"
	}

	offset := (page - 1) * pageSize

	// Build query - use subqueries to avoid JOIN inflation
	query := `
		SELECT l.id, l.org_id, l.phone, l.name, l.email, l.status, l.source,
			   l.interested_services, l.tags, l.notes, l.created_at, l.updated_at,
			   (SELECT COALESCE(SUM(p.amount_cents), 0) FROM payments p WHERE p.lead_id = l.id AND p.status = 'succeeded') as payment_total,
			   (SELECT COUNT(*) FROM bookings b WHERE b.lead_id = l.id) as booking_count
		FROM leads l
		WHERE l.org_id = $1
	`
	args := []any{orgID}
	argNum := 2

	if status != "" {
		query += " AND l.status = $" + strconv.Itoa(argNum)
		args = append(args, status)
		argNum++
	}

	if search != "" {
		query += " AND (l.name ILIKE $" + strconv.Itoa(argNum) + " OR l.phone ILIKE $" + strconv.Itoa(argNum) + " OR l.email ILIKE $" + strconv.Itoa(argNum) + ")"
		args = append(args, "%"+search+"%")
		argNum++
	}

	// Add sorting
	validSortColumns := map[string]bool{
		"created_at": true,
		"updated_at": true,
		"name":       true,
		"status":     true,
	}
	if !validSortColumns[sortBy] {
		sortBy = "created_at"
	}
	query += " ORDER BY l." + sortBy + " " + strings.ToUpper(sortOrder)

	// Get total count first
	countQuery := `SELECT COUNT(*) FROM leads WHERE org_id = $1`
	if status != "" {
		countQuery += " AND status = $2"
	}
	var total int
	countArgs := []any{orgID}
	if status != "" {
		countArgs = append(countArgs, status)
	}
	if err := h.db.QueryRowContext(r.Context(), countQuery, countArgs...).Scan(&total); err != nil {
		h.logger.Error("failed to count leads", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Add pagination
	query += " LIMIT $" + strconv.Itoa(argNum) + " OFFSET $" + strconv.Itoa(argNum+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query leads", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var leads []LeadResponse
	for rows.Next() {
		var lead LeadResponse
		var name, email, source, notes sql.NullString
		var interestedServices, tags []byte
		var createdAt, updatedAt time.Time

		err := rows.Scan(
			&lead.ID, &lead.OrgID, &lead.Phone, &name, &email, &lead.Status, &source,
			&interestedServices, &tags, &notes, &createdAt, &updatedAt,
			&lead.PaymentTotal, &lead.BookingCount,
		)
		if err != nil {
			h.logger.Error("failed to scan lead", "error", err)
			continue
		}

		lead.Name = name.String
		lead.Email = email.String
		lead.Source = source.String
		lead.Notes = notes.String
		lead.CreatedAt = createdAt.Format(time.RFC3339)
		lead.UpdatedAt = updatedAt.Format(time.RFC3339)

		json.Unmarshal(interestedServices, &lead.InterestedServices)
		json.Unmarshal(tags, &lead.Tags)

		// Get conversation job count via phone pattern
		// conversation_id format: "sms:{orgID}:{phone}"
		normalizedPhone := normalizePhoneDigits(lead.Phone)
		conversationPattern := "sms:" + orgID + ":%" + normalizedPhone + "%"
		var jobCount int
		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM conversation_jobs WHERE conversation_id LIKE $1`,
			conversationPattern,
		).Scan(&jobCount)
		lead.ConversationJobCount = jobCount

		// Get last contact from conversation_jobs
		var lastContact sql.NullTime
		h.db.QueryRowContext(r.Context(),
			`SELECT MAX(created_at) FROM conversation_jobs WHERE conversation_id LIKE $1`,
			conversationPattern,
		).Scan(&lastContact)
		if lastContact.Valid {
			formatted := lastContact.Time.Format(time.RFC3339)
			lead.LastContactAt = &formatted
		}

		leads = append(leads, lead)
	}

	if leads == nil {
		leads = []LeadResponse{}
	}

	totalPages := (total + pageSize - 1) / pageSize
	response := LeadsListResponse{
		Leads:      leads,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// normalizePhoneDigits extracts just the digits from a phone number
func normalizePhoneDigits(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	return digits.String()
}

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
	json.Unmarshal(interestedServices, &lead.InterestedServices)
	json.Unmarshal(tags, &lead.Tags)

	// Get distinct conversation IDs from conversation_jobs via phone
	normalizedPhone := normalizePhoneDigits(lead.Phone)
	conversationPattern := "sms:" + orgID + ":%" + normalizedPhone + "%"

	convIDRows, err := h.db.QueryContext(r.Context(),
		`SELECT DISTINCT conversation_id FROM conversation_jobs WHERE conversation_id LIKE $1 ORDER BY conversation_id`,
		conversationPattern,
	)
	if err == nil {
		defer convIDRows.Close()
		for convIDRows.Next() {
			var convID string
			if convIDRows.Scan(&convID) == nil {
				lead.ConversationIDs = append(lead.ConversationIDs, convID)
			}
		}
	}

	// Get job count
	var jobCount int
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM conversation_jobs WHERE conversation_id LIKE $1`,
		conversationPattern,
	).Scan(&jobCount)
	lead.ConversationJobCount = jobCount

	// Get payments
	payQuery := `
		SELECT id, amount_cents, status, created_at
		FROM payments
		WHERE lead_id = $1
		ORDER BY created_at DESC
		LIMIT 10
	`
	payRows, err := h.db.QueryContext(r.Context(), payQuery, leadUUID)
	if err == nil {
		defer payRows.Close()
		for payRows.Next() {
			var pay PaymentSummary
			var payCreatedAt time.Time
			err := payRows.Scan(&pay.ID, &pay.AmountCents, &pay.Status, &payCreatedAt)
			if err == nil {
				pay.CreatedAt = payCreatedAt.Format(time.RFC3339)
				lead.Payments = append(lead.Payments, pay)
			}
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
	if err == nil {
		defer bookRows.Close()
		for bookRows.Next() {
			var book BookingSummary
			var scheduledAt time.Time
			err := bookRows.Scan(&book.ID, &book.Service, &scheduledAt, &book.Status)
			if err == nil {
				book.ScheduledAt = scheduledAt.Format(time.RFC3339)
				lead.Bookings = append(lead.Bookings, book)
			}
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

// UpdateLeadRequest contains fields for updating a lead.
type UpdateLeadRequest struct {
	Name               *string  `json:"name,omitempty"`
	Email              *string  `json:"email,omitempty"`
	Status             *string  `json:"status,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	Notes              *string  `json:"notes,omitempty"`
	InterestedServices []string `json:"interested_services,omitempty"`
}

// UpdateLead updates a lead's information.
// PATCH /admin/orgs/{orgID}/leads/{leadID}
func (h *AdminLeadsHandler) UpdateLead(w http.ResponseWriter, r *http.Request) {
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

	var req UpdateLeadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Build update query dynamically
	updates := []string{}
	args := []any{}
	argNum := 1

	if req.Name != nil {
		updates = append(updates, "name = $"+strconv.Itoa(argNum))
		args = append(args, *req.Name)
		argNum++
	}
	if req.Email != nil {
		updates = append(updates, "email = $"+strconv.Itoa(argNum))
		args = append(args, *req.Email)
		argNum++
	}
	if req.Status != nil {
		updates = append(updates, "status = $"+strconv.Itoa(argNum))
		args = append(args, *req.Status)
		argNum++
	}
	if req.Notes != nil {
		updates = append(updates, "notes = $"+strconv.Itoa(argNum))
		args = append(args, *req.Notes)
		argNum++
	}
	if req.Tags != nil {
		tagsJSON, _ := json.Marshal(req.Tags)
		updates = append(updates, "tags = $"+strconv.Itoa(argNum))
		args = append(args, tagsJSON)
		argNum++
	}
	if req.InterestedServices != nil {
		servicesJSON, _ := json.Marshal(req.InterestedServices)
		updates = append(updates, "interested_services = $"+strconv.Itoa(argNum))
		args = append(args, servicesJSON)
		argNum++
	}

	if len(updates) == 0 {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	updates = append(updates, "updated_at = $"+strconv.Itoa(argNum))
	args = append(args, time.Now())
	argNum++

	args = append(args, leadUUID, orgID)

	query := "UPDATE leads SET " + strings.Join(updates, ", ") +
		" WHERE id = $" + strconv.Itoa(argNum) + " AND org_id = $" + strconv.Itoa(argNum+1)

	result, err := h.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to update lead", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		http.Error(w, "lead not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// LeadStatsResponse contains aggregated lead statistics.
type LeadStatsResponse struct {
	TotalLeads     int            `json:"total_leads"`
	ByStatus       map[string]int `json:"by_status"`
	NewThisWeek    int            `json:"new_this_week"`
	NewThisMonth   int            `json:"new_this_month"`
	ConversionRate float64        `json:"conversion_rate"`
}

// GetLeadStats returns aggregated lead statistics.
// GET /admin/orgs/{orgID}/leads/stats
func (h *AdminLeadsHandler) GetLeadStats(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing orgID", http.StatusBadRequest)
		return
	}

	stats := LeadStatsResponse{
		ByStatus: make(map[string]int),
	}

	// Total leads
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1`, orgID,
	).Scan(&stats.TotalLeads)

	// By status
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT status, COUNT(*) FROM leads WHERE org_id = $1 GROUP BY status`, orgID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int
			if rows.Scan(&status, &count) == nil {
				stats.ByStatus[status] = count
			}
		}
	}

	// New this week
	weekAgo := time.Now().AddDate(0, 0, -7)
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1 AND created_at >= $2`, orgID, weekAgo,
	).Scan(&stats.NewThisWeek)

	// New this month
	monthAgo := time.Now().AddDate(0, -1, 0)
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM leads WHERE org_id = $1 AND created_at >= $2`, orgID, monthAgo,
	).Scan(&stats.NewThisMonth)

	// Conversion rate (leads with successful payments / total leads)
	var converted int
	h.db.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT l.id) FROM leads l
		 JOIN payments p ON l.id = p.lead_id
		 WHERE l.org_id = $1 AND p.status = 'succeeded'`, orgID,
	).Scan(&converted)
	if stats.TotalLeads > 0 {
		stats.ConversionRate = float64(converted) / float64(stats.TotalLeads) * 100
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
