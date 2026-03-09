package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

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
