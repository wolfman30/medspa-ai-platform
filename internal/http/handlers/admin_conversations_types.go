package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

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
	Channel              string  `json:"channel"` // "sms" or "voice"
	CustomerPhone        string  `json:"customer_phone"`
	CustomerName         string  `json:"customer_name"`
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
	Channel       string            `json:"channel"` // "sms" or "voice"
	CustomerPhone string            `json:"customer_phone"`
	CustomerName  string            `json:"customer_name"`
	Status        string            `json:"status"`
	StartedAt     string            `json:"started_at"`
	LastMessageAt *string           `json:"last_message_at,omitempty"`
	Messages      []MessageResponse `json:"messages"`
	Metadata      ConversationMeta  `json:"metadata"`
}

// MessageResponse represents a message in a conversation.
type MessageResponse struct {
	ID                string `json:"id"`
	Role              string `json:"role"`
	Content           string `json:"content"`
	Timestamp         string `json:"timestamp"`
	From              string `json:"from,omitempty"`
	To                string `json:"to,omitempty"`
	ProviderMessageID string `json:"provider_message_id,omitempty"`
	Status            string `json:"status,omitempty"`
	ErrorReason       string `json:"error_reason,omitempty"`
}

// ConversationMeta contains metadata about a conversation.
type ConversationMeta struct {
	TotalMessages    int    `json:"total_messages"`
	CustomerMessages int    `json:"customer_messages"`
	AIMessages       int    `json:"ai_messages"`
	Source           string `json:"source"` // "database" or "redis"
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

// easternLocation is the pre-loaded America/New_York timezone for formatting timestamps.
var easternLocation = loadEasternLocation()

func loadEasternLocation() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.FixedZone("EST", -5*60*60)
	}
	return loc
}

// formatTimeEastern formats a time in Eastern timezone as RFC3339.
func formatTimeEastern(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(easternLocation).Format(time.RFC3339)
}

// channelFromConversationID extracts "sms" or "voice" from the conversation ID prefix.
func channelFromConversationID(id string) string {
	if strings.HasPrefix(id, "voice:") {
		return "voice"
	}
	return "sms"
}

// parseConversationID extracts orgID and phone/session from conversation ID format
// "sms:{orgID}:{phone}" or "voice:{orgID}:{sessionID}".
func parseConversationID(conversationID string) (orgID, phone string, ok bool) {
	parts := strings.Split(conversationID, ":")
	if len(parts) < 3 {
		return "", "", false
	}
	prefix := parts[0]
	if prefix != "sms" && prefix != "voice" {
		return "", "", false
	}
	// Rejoin remaining parts in case session ID contains colons
	return parts[1], strings.Join(parts[2:], ":"), true
}

// jsonError writes a JSON error response.
func jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// hasConversationsTable checks if the conversations table exists.
func (h *AdminConversationsHandler) hasConversationsTable(ctx *http.Request) bool {
	var exists bool
	h.db.QueryRowContext(ctx.Context(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'conversations')`,
	).Scan(&exists)
	return exists
}

// hasConversationsForOrg checks if the conversations table has any data for the given org.
func (h *AdminConversationsHandler) hasConversationsForOrg(ctx *http.Request, orgID string) bool {
	if !h.hasConversationsTable(ctx) {
		return false
	}
	var count int
	h.db.QueryRowContext(ctx.Context(),
		`SELECT COUNT(*) FROM conversations WHERE org_id = $1 LIMIT 1`, orgID,
	).Scan(&count)
	return count > 0
}

// getMessagesFromDB retrieves conversation messages from the database ordered by creation time.
func (h *AdminConversationsHandler) getMessagesFromDB(r *http.Request, conversationID string) ([]MessageResponse, error) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, role, content, from_phone, to_phone, provider_message_id, status, error_reason, created_at
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
		var providerMessageID, status, errorReason sql.NullString
		var createdAt time.Time

		if err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &fromPhone, &toPhone, &providerMessageID, &status, &errorReason, &createdAt); err != nil {
			continue
		}

		msg.Timestamp = formatTimeEastern(createdAt)
		if providerMessageID.Valid {
			msg.ProviderMessageID = providerMessageID.String
		}
		if status.Valid {
			msg.Status = status.String
		}
		if errorReason.Valid {
			msg.ErrorReason = errorReason.String
		}
		msg.From = fromPhone.String
		msg.To = toPhone.String
		messages = append(messages, msg)
	}

	return messages, nil
}
