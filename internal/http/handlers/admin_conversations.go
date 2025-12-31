package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// AdminConversationsHandler handles admin API endpoints for conversation viewing.
// Uses conversations/conversation_messages tables for long-term history, with fallback to Redis for recent messages.
type AdminConversationsHandler struct {
	db              *sql.DB
	transcriptStore *conversation.SMSTranscriptStore
	logger          *logging.Logger
}

// NewAdminConversationsHandler creates a new admin conversations handler.
func NewAdminConversationsHandler(db *sql.DB, transcriptStore *conversation.SMSTranscriptStore, logger *logging.Logger) *AdminConversationsHandler {
	if logger == nil {
		logger = logging.Default()
	}
	return &AdminConversationsHandler{
		db:              db,
		transcriptStore: transcriptStore,
		logger:          logger,
	}
}

// ConversationListItem represents a conversation in list responses.
type ConversationListItem struct {
	ID                   string  `json:"id"`
	OrgID                string  `json:"org_id"`
	CustomerPhone        string  `json:"customer_phone"`
	Status               string  `json:"status"`
	MessageCount         int     `json:"message_count"`
	CustomerMessageCount int     `json:"customer_message_count"`
	AIMessageCount       int     `json:"ai_message_count"`
	StartedAt            string  `json:"started_at"`
	LastMessageAt        *string `json:"last_message_at,omitempty"`
}

// ConversationsListResponse represents a paginated list of conversations.
type ConversationsListResponse struct {
	Conversations []ConversationListItem `json:"conversations"`
	Total         int                    `json:"total"`
	Page          int                    `json:"page"`
	PageSize      int                    `json:"page_size"`
	TotalPages    int                    `json:"total_pages"`
}

// ConversationDetailResponse represents detailed conversation information.
type ConversationDetailResponse struct {
	ID            string            `json:"id"`
	OrgID         string            `json:"org_id"`
	CustomerPhone string            `json:"customer_phone"`
	Status        string            `json:"status"`
	StartedAt     string            `json:"started_at"`
	LastMessageAt *string           `json:"last_message_at,omitempty"`
	Messages      []MessageResponse `json:"messages"`
	Metadata      ConversationMeta  `json:"metadata"`
}

// MessageResponse represents a message in a conversation.
type MessageResponse struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

// ConversationMeta contains metadata about a conversation.
type ConversationMeta struct {
	TotalMessages    int    `json:"total_messages"`
	CustomerMessages int    `json:"customer_messages"`
	AIMessages       int    `json:"ai_messages"`
	Source           string `json:"source"` // "database" or "redis"
}

// parseConversationID extracts orgID and phone from conversation ID format "sms:{orgID}:{phone}"
func parseConversationID(conversationID string) (orgID, phone string, ok bool) {
	parts := strings.Split(conversationID, ":")
	if len(parts) != 3 || parts[0] != "sms" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// hasConversationsTable checks if the conversations table exists
func (h *AdminConversationsHandler) hasConversationsTable(ctx *http.Request) bool {
	var exists bool
	h.db.QueryRowContext(ctx.Context(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'conversations')`,
	).Scan(&exists)
	return exists
}

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
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	phone := r.URL.Query().Get("phone")
	status := r.URL.Query().Get("status")
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")

	offset := (page - 1) * pageSize

	// Try conversations table first (long-term history)
	if h.hasConversationsTable(r) {
		h.listFromConversationsTable(w, r, orgID, phone, status, dateFrom, dateTo, page, pageSize, offset)
		return
	}

	// Fallback to conversation_jobs
	h.listFromConversationJobs(w, r, orgID, phone, dateFrom, dateTo, page, pageSize, offset)
}

func (h *AdminConversationsHandler) listFromConversationsTable(w http.ResponseWriter, r *http.Request, orgID, phone, status, dateFrom, dateTo string, page, pageSize, offset int) {
	query := `
		SELECT conversation_id, org_id, phone, status,
			   message_count, customer_message_count, ai_message_count,
			   started_at, last_message_at
		FROM conversations
		WHERE org_id = $1
	`
	args := []any{orgID}
	argNum := 2

	if phone != "" {
		query += " AND phone LIKE $" + strconv.Itoa(argNum)
		args = append(args, "%"+phone+"%")
		argNum++
	}
	if status != "" {
		query += " AND status = $" + strconv.Itoa(argNum)
		args = append(args, status)
		argNum++
	}
	if dateFrom != "" {
		if t, err := time.Parse("2006-01-02", dateFrom); err == nil {
			query += " AND started_at >= $" + strconv.Itoa(argNum)
			args = append(args, t)
			argNum++
		}
	}
	if dateTo != "" {
		if t, err := time.Parse("2006-01-02", dateTo); err == nil {
			query += " AND started_at < $" + strconv.Itoa(argNum)
			args = append(args, t.AddDate(0, 0, 1))
			argNum++
		}
	}

	query += " ORDER BY COALESCE(last_message_at, started_at) DESC"

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
			&startedAt, &lastMessageAt,
		)
		if err != nil {
			h.logger.Error("failed to scan conversation", "error", err)
			continue
		}

		conv.StartedAt = startedAt.Format(time.RFC3339)
		if lastMessageAt.Valid {
			formatted := lastMessageAt.Time.Format(time.RFC3339)
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

func (h *AdminConversationsHandler) listFromConversationJobs(w http.ResponseWriter, r *http.Request, orgID, phone, dateFrom, dateTo string, page, pageSize, offset int) {
	conversationIDPattern := "sms:" + orgID + ":%"

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

		lastFormatted := lastActivity.Format(time.RFC3339)
		conversations = append(conversations, ConversationListItem{
			ID:            conversationID,
			OrgID:         parsedOrgID,
			CustomerPhone: customerPhone,
			Status:        status,
			MessageCount:  jobCount,
			StartedAt:     firstActivity.Format(time.RFC3339),
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

// GetConversation returns detailed information about a specific conversation.
// GET /admin/orgs/{orgID}/conversations/{conversationID}
func (h *AdminConversationsHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	conversationID := chi.URLParam(r, "conversationID")

	if orgID == "" || conversationID == "" {
		http.Error(w, "missing orgID or conversationID", http.StatusBadRequest)
		return
	}

	parsedOrgID, customerPhone, ok := parseConversationID(conversationID)
	if !ok || parsedOrgID != orgID {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}

	conv := ConversationDetailResponse{
		ID:            conversationID,
		OrgID:         parsedOrgID,
		CustomerPhone: customerPhone,
		Messages:      []MessageResponse{},
	}

	// Try to get conversation from conversations table
	var startedAt time.Time
	var lastMessageAt sql.NullTime
	err := h.db.QueryRowContext(r.Context(), `
		SELECT status, message_count, customer_message_count, ai_message_count, started_at, last_message_at
		FROM conversations WHERE conversation_id = $1
	`, conversationID).Scan(&conv.Status, &conv.Metadata.TotalMessages, &conv.Metadata.CustomerMessages, &conv.Metadata.AIMessages, &startedAt, &lastMessageAt)

	if err == nil {
		conv.StartedAt = startedAt.Format(time.RFC3339)
		if lastMessageAt.Valid {
			formatted := lastMessageAt.Time.Format(time.RFC3339)
			conv.LastMessageAt = &formatted
		}

		// Get messages from conversation_messages table
		messages, _ := h.getMessagesFromDB(r, conversationID)
		if len(messages) > 0 {
			conv.Messages = messages
			conv.Metadata.Source = "database"
		}
	}

	// If no messages from DB, try Redis
	if len(conv.Messages) == 0 && h.transcriptStore != nil {
		messages, err := h.transcriptStore.List(r.Context(), conversationID, 0)
		if err == nil && len(messages) > 0 {
			conv.Metadata.Source = "redis"
			conv.Metadata.TotalMessages = 0
			conv.Metadata.CustomerMessages = 0
			conv.Metadata.AIMessages = 0

			for _, msg := range messages {
				conv.Messages = append(conv.Messages, MessageResponse{
					ID:        msg.ID,
					Role:      msg.Role,
					Content:   msg.Body,
					Timestamp: msg.Timestamp.Format(time.RFC3339),
					From:      msg.From,
					To:        msg.To,
				})
				conv.Metadata.TotalMessages++
				if msg.Role == "user" {
					conv.Metadata.CustomerMessages++
				} else if msg.Role == "assistant" {
					conv.Metadata.AIMessages++
				}
			}
		}
	}

	if conv.Status == "" {
		conv.Status = "active"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conv)
}

func (h *AdminConversationsHandler) getMessagesFromDB(r *http.Request, conversationID string) ([]MessageResponse, error) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, role, content, from_phone, to_phone, created_at
		FROM conversation_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageResponse
	for rows.Next() {
		var msg MessageResponse
		var fromPhone, toPhone sql.NullString
		var createdAt time.Time

		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &fromPhone, &toPhone, &createdAt); err != nil {
			continue
		}

		msg.Timestamp = createdAt.Format(time.RFC3339)
		msg.From = fromPhone.String
		msg.To = toPhone.String
		messages = append(messages, msg)
	}

	return messages, nil
}

// ConversationStatsResponse contains aggregated conversation statistics.
type ConversationStatsResponse struct {
	TotalConversations int            `json:"total_conversations"`
	TotalMessages      int            `json:"total_messages"`
	ByStatus           map[string]int `json:"by_status"`
	TodayCount         int            `json:"today_count"`
	WeekCount          int            `json:"week_count"`
	MonthCount         int            `json:"month_count"`
	SixMonthCount      int            `json:"six_month_count"`
}

// GetConversationStats returns aggregated conversation statistics.
// GET /admin/orgs/{orgID}/conversations/stats
func (h *AdminConversationsHandler) GetConversationStats(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	if orgID == "" {
		http.Error(w, "missing orgID", http.StatusBadRequest)
		return
	}

	stats := ConversationStatsResponse{
		ByStatus: make(map[string]int),
	}

	now := time.Now()
	today := now.Truncate(24 * time.Hour)
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, -1, 0)
	sixMonthsAgo := now.AddDate(0, -6, 0)

	// Try conversations table first
	if h.hasConversationsTable(r) {
		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM conversations WHERE org_id = $1`, orgID,
		).Scan(&stats.TotalConversations)

		h.db.QueryRowContext(r.Context(),
			`SELECT COALESCE(SUM(message_count), 0) FROM conversations WHERE org_id = $1`, orgID,
		).Scan(&stats.TotalMessages)

		rows, _ := h.db.QueryContext(r.Context(),
			`SELECT status, COUNT(*) FROM conversations WHERE org_id = $1 GROUP BY status`, orgID,
		)
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var status string
				var count int
				if rows.Scan(&status, &count) == nil {
					stats.ByStatus[status] = count
				}
			}
		}

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM conversations WHERE org_id = $1 AND started_at >= $2`, orgID, today,
		).Scan(&stats.TodayCount)

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM conversations WHERE org_id = $1 AND started_at >= $2`, orgID, weekAgo,
		).Scan(&stats.WeekCount)

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM conversations WHERE org_id = $1 AND started_at >= $2`, orgID, monthAgo,
		).Scan(&stats.MonthCount)

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM conversations WHERE org_id = $1 AND started_at >= $2`, orgID, sixMonthsAgo,
		).Scan(&stats.SixMonthCount)
	} else {
		// Fallback to conversation_jobs
		conversationIDPattern := "sms:" + orgID + ":%"

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1`,
			conversationIDPattern,
		).Scan(&stats.TotalConversations)

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM conversation_jobs WHERE conversation_id LIKE $1`,
			conversationIDPattern,
		).Scan(&stats.TotalMessages)

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1 AND created_at >= $2`,
			conversationIDPattern, today,
		).Scan(&stats.TodayCount)

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1 AND created_at >= $2`,
			conversationIDPattern, weekAgo,
		).Scan(&stats.WeekCount)

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1 AND created_at >= $2`,
			conversationIDPattern, monthAgo,
		).Scan(&stats.MonthCount)

		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(DISTINCT conversation_id) FROM conversation_jobs WHERE conversation_id LIKE $1 AND created_at >= $2`,
			conversationIDPattern, sixMonthsAgo,
		).Scan(&stats.SixMonthCount)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// ExportTranscript exports a conversation transcript as plain text.
// GET /admin/orgs/{orgID}/conversations/{conversationID}/export
func (h *AdminConversationsHandler) ExportTranscript(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	conversationID := chi.URLParam(r, "conversationID")

	if orgID == "" || conversationID == "" {
		http.Error(w, "missing orgID or conversationID", http.StatusBadRequest)
		return
	}

	parsedOrgID, customerPhone, ok := parseConversationID(conversationID)
	if !ok || parsedOrgID != orgID {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}

	// Get start time from conversations table or conversation_jobs
	var startedAt time.Time
	h.db.QueryRowContext(r.Context(),
		`SELECT started_at FROM conversations WHERE conversation_id = $1`, conversationID,
	).Scan(&startedAt)
	if startedAt.IsZero() {
		h.db.QueryRowContext(r.Context(),
			`SELECT MIN(created_at) FROM conversation_jobs WHERE conversation_id = $1`, conversationID,
		).Scan(&startedAt)
	}

	// Build transcript header
	transcript := "Conversation Transcript\n"
	transcript += "========================\n\n"
	transcript += "Customer Phone: " + customerPhone + "\n"
	if !startedAt.IsZero() {
		transcript += "Started: " + startedAt.Format(time.RFC1123) + "\n"
	}
	transcript += "Conversation ID: " + conversationID + "\n\n"
	transcript += "--- Messages ---\n\n"

	// Try to get messages from database first
	messages, _ := h.getMessagesFromDB(r, conversationID)
	if len(messages) > 0 {
		for _, msg := range messages {
			roleLabel := msg.Role
			if roleLabel == "assistant" {
				roleLabel = "AI"
			} else if roleLabel == "user" {
				roleLabel = "Customer"
			}
			timestamp, _ := time.Parse(time.RFC3339, msg.Timestamp)
			transcript += "[" + timestamp.Format("2006-01-02 15:04:05") + "] " + roleLabel + ":\n"
			transcript += msg.Content + "\n\n"
		}
	} else if h.transcriptStore != nil {
		// Fallback to Redis
		redisMessages, err := h.transcriptStore.List(r.Context(), conversationID, 0)
		if err == nil {
			for _, msg := range redisMessages {
				roleLabel := msg.Role
				if roleLabel == "assistant" {
					roleLabel = "AI"
				} else if roleLabel == "user" {
					roleLabel = "Customer"
				}
				transcript += "[" + msg.Timestamp.Format("2006-01-02 15:04:05") + "] " + roleLabel + ":\n"
				transcript += msg.Body + "\n\n"
			}
		}
	}

	if len(messages) == 0 && h.transcriptStore == nil {
		transcript += "(No messages found)\n"
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", "attachment; filename=transcript-"+conversationID+".txt")
	w.Write([]byte(transcript))
}
