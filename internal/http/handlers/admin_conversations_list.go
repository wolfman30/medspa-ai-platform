package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// ListConversations returns a paginated list of conversations for an organization.
// GET /admin/orgs/{orgID}/conversations
func (h *AdminConversationsHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
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
	if pageSize < 1 || pageSize > MaxPageSize {
		pageSize = DefaultPageSize
	}
	phone := r.URL.Query().Get("phone")
	status := r.URL.Query().Get("status")
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")

	offset := (page - 1) * pageSize

	// Try conversations table first (long-term history) - only if it has data for this org
	if h.hasConversationsForOrg(r, orgID) {
		h.listFromConversationsTable(w, r, orgID, phone, status, dateFrom, dateTo, page, pageSize, offset)
		return
	}

	// Fallback to conversation_jobs (used when conversations table is empty or doesn't exist)
	h.listFromConversationJobs(w, r, orgID, phone, dateFrom, dateTo, page, pageSize, offset)
}

// listFromConversationsTable lists conversations from the long-term conversations table.
func (h *AdminConversationsHandler) listFromConversationsTable(w http.ResponseWriter, r *http.Request, orgID, phone, status, dateFrom, dateTo string, page, pageSize, offset int) {
	query := `
		SELECT conversations.conversation_id, conversations.org_id, conversations.phone, conversations.status,
			   conversations.message_count, conversations.customer_message_count, conversations.ai_message_count,
			   conversations.started_at, conversations.last_message_at,
			   COALESCE(NULLIF(leads.name, ''), conversations.customer_name, '') as customer_name
		FROM conversations
		LEFT JOIN leads ON conversations.lead_id = leads.id
		WHERE conversations.org_id = $1
	`
	args := []any{orgID}
	argNum := 2

	if phone != "" {
		query += " AND conversations.phone LIKE $" + strconv.Itoa(argNum)
		args = append(args, "%"+phone+"%")
		argNum++
	}
	if status != "" {
		query += " AND conversations.status = $" + strconv.Itoa(argNum)
		args = append(args, status)
		argNum++
	}
	if dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			query += " AND conversations.started_at >= $" + strconv.Itoa(argNum)
			args = append(args, t)
			argNum++
		}
	}
	if dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			query += " AND conversations.started_at < $" + strconv.Itoa(argNum)
			args = append(args, t.AddDate(0, 0, 1))
			argNum++
		}
	}

	query += " ORDER BY COALESCE(conversations.last_message_at, conversations.started_at) DESC"

	// Get total count
	countQuery := "SELECT COUNT(*) FROM conversations WHERE org_id = $1"
	var total int
	h.db.QueryRowContext(r.Context(), countQuery, orgID).Scan(&total)

	// Add pagination
	query += " LIMIT $" + strconv.Itoa(argNum) + " OFFSET $" + strconv.Itoa(argNum+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query conversations", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var conversations []ConversationListItem
	for rows.Next() {
		var conv ConversationListItem
		var lastMessageAt sql.NullTime
		var startedAt time.Time

		err := rows.Scan(
			&conv.ID, &conv.OrgID, &conv.CustomerPhone, &conv.Status,
			&conv.MessageCount, &conv.CustomerMessageCount, &conv.AIMessageCount,
			&startedAt, &lastMessageAt, &conv.CustomerName,
		)
		if err != nil {
			h.logger.Error("failed to scan conversation", "error", err)
			continue
		}

		conv.StartedAt = formatTimeEastern(startedAt)
		if lastMessageAt.Valid {
			formatted := formatTimeEastern(lastMessageAt.Time)
			conv.LastMessageAt = &formatted
		}

		conversations = append(conversations, conv)
	}

	if conversations == nil {
		conversations = []ConversationListItem{}
	}

	totalPages := (total + pageSize - 1) / pageSize
	response := ConversationsListResponse{
		Conversations: conversations,
		Total:         total,
		Page:          page,
		PageSize:      pageSize,
		TotalPages:    totalPages,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// listFromConversationJobs lists conversations from the conversation_jobs fallback table.
func (h *AdminConversationsHandler) listFromConversationJobs(w http.ResponseWriter, r *http.Request, orgID, phone, dateFrom, dateTo string, page, pageSize, offset int) {
	// Match both sms: and voice: conversation IDs for this org
	conversationIDPattern := "%:" + orgID + ":%"

	query := `
		SELECT conversation_id,
			   COUNT(*) as job_count,
			   MAX(created_at) as last_activity,
			   MIN(created_at) as first_activity,
			   MAX(status) as last_status
		FROM conversation_jobs
		WHERE conversation_id LIKE $1
	`
	args := []any{conversationIDPattern}
	argNum := 2

	if phone != "" {
		query += " AND conversation_id LIKE $" + strconv.Itoa(argNum)
		args = append(args, "%"+phone+"%")
		argNum++
	}
	if dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			query += " AND created_at >= $" + strconv.Itoa(argNum)
			args = append(args, t)
			argNum++
		}
	}
	if dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			query += " AND created_at < $" + strconv.Itoa(argNum)
			args = append(args, t.AddDate(0, 0, 1))
			argNum++
		}
	}

	query += " GROUP BY conversation_id ORDER BY MAX(created_at) DESC"

	countQuery := `SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1`
	var total int
	h.db.QueryRowContext(r.Context(), countQuery, conversationIDPattern).Scan(&total)

	query += " LIMIT $" + strconv.Itoa(argNum) + " OFFSET $" + strconv.Itoa(argNum+1)
	args = append(args, pageSize, offset)

	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query conversations from jobs", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var conversations []ConversationListItem
	for rows.Next() {
		var conversationID string
		var jobCount int
		var lastActivity, firstActivity time.Time
		var status string

		if err := rows.Scan(&conversationID, &jobCount, &lastActivity, &firstActivity, &status); err != nil {
			continue
		}

		parsedOrgID, customerPhone, ok := parseConversationID(conversationID)
		if !ok {
			continue
		}

		// Try to look up patient name from leads table by phone
		var customerName string
		h.db.QueryRowContext(r.Context(),
			`SELECT COALESCE(name, '') FROM leads WHERE org_id = $1 AND phone = $2 LIMIT 1`,
			parsedOrgID, customerPhone,
		).Scan(&customerName)

		lastFormatted := formatTimeEastern(lastActivity)
		conversations = append(conversations, ConversationListItem{
			ID:            conversationID,
			OrgID:         parsedOrgID,
			Channel:       channelFromConversationID(conversationID),
			CustomerPhone: customerPhone,
			CustomerName:  customerName,
			Status:        status,
			MessageCount:  jobCount,
			StartedAt:     formatTimeEastern(firstActivity),
			LastMessageAt: &lastFormatted,
		})
	}

	if conversations == nil {
		conversations = []ConversationListItem{}
	}

	totalPages := (total + pageSize - 1) / pageSize
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ConversationsListResponse{
		Conversations: conversations,
		Total:         total,
		Page:          page,
		PageSize:      pageSize,
		TotalPages:    totalPages,
	})
}
