package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
)

// GetConversation returns detailed information about a specific conversation.
// GET /admin/orgs/{orgID}/conversations/{conversationID}
func (h *AdminConversationsHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	orgID := chi.URLParam(r, "orgID")
	conversationID := chi.URLParam(r, "conversationID")

	if orgID == "" || conversationID == "" {
		jsonError(w, "missing orgID or conversationID", http.StatusBadRequest)
		return
	}

	// URL-decode the conversation ID since it may contain encoded colons (%3A)
	if decoded, err := url.PathUnescape(conversationID); err == nil {
		conversationID = decoded
	}

	parsedOrgID, customerPhone, ok := parseConversationID(conversationID)
	if !ok || parsedOrgID != orgID {
		jsonError(w, fmt.Sprintf("invalid conversation ID format: %s (expected sms:orgID:phone or voice:orgID:session)", conversationID), http.StatusNotFound)
		return
	}

	conv := ConversationDetailResponse{
		ID:            conversationID,
		OrgID:         parsedOrgID,
		Channel:       channelFromConversationID(conversationID),
		CustomerPhone: customerPhone,
		Messages:      []MessageResponse{},
	}

	// Try to look up patient name from leads table
	h.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(l.name, '') FROM conversations c LEFT JOIN leads l ON c.lead_id = l.id WHERE c.conversation_id = $1`,
		conversationID,
	).Scan(&conv.CustomerName)
	if conv.CustomerName == "" {
		// Fallback: look up by phone
		h.db.QueryRowContext(r.Context(),
			`SELECT COALESCE(name, '') FROM leads WHERE org_id = $1 AND phone = $2 LIMIT 1`,
			parsedOrgID, customerPhone,
		).Scan(&conv.CustomerName)
	}

	// Try to get conversation from conversations table
	var startedAt time.Time
	var lastMessageAt sql.NullTime
	err := h.db.QueryRowContext(r.Context(), `
		SELECT status, message_count, customer_message_count, ai_message_count, started_at, last_message_at
		FROM conversations WHERE conversation_id = $1
	`, conversationID).Scan(&conv.Status, &conv.Metadata.TotalMessages, &conv.Metadata.CustomerMessages, &conv.Metadata.AIMessages, &startedAt, &lastMessageAt)

	if err == nil {
		conv.StartedAt = formatTimeEastern(startedAt)
		if lastMessageAt.Valid {
			formatted := formatTimeEastern(lastMessageAt.Time)
			conv.LastMessageAt = &formatted
		}

		// Get messages from conversation_messages table
		messages, _ := h.getMessagesFromDB(r, conversationID)
		if len(messages) > 0 {
			conv.Messages = messages
			conv.Metadata.Source = "database"
		}
	} else {
		// Fallback: try to get started_at from conversation_jobs
		var jobStartedAt time.Time
		h.db.QueryRowContext(r.Context(),
			`SELECT MIN(created_at) FROM conversation_jobs WHERE conversation_id = $1`,
			conversationID,
		).Scan(&jobStartedAt)
		if !jobStartedAt.IsZero() {
			conv.StartedAt = formatTimeEastern(jobStartedAt)
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
					ID:                msg.ID,
					Role:              msg.Role,
					Content:           msg.Body,
					Timestamp:         formatTimeEastern(msg.Timestamp),
					From:              msg.From,
					To:                msg.To,
					ProviderMessageID: msg.ProviderMessageID,
					Status:            msg.Status,
					ErrorReason:       msg.ErrorReason,
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
