package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
)

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

	// Try conversations table first - only if it has data for this org
	if h.hasConversationsForOrg(r, orgID) {
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
		conversationIDPattern := "%:" + orgID + ":%"

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

	// URL-decode the conversation ID since it may contain encoded colons (%3A)
	if decoded, err := url.PathUnescape(conversationID); err == nil {
		conversationID = decoded
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
		transcript += "Started: " + startedAt.In(easternLocation).Format(time.RFC1123) + "\n"
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
			transcript += "[" + timestamp.In(easternLocation).Format("2006-01-02 15:04:05") + "] " + roleLabel + ":\n"
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
				transcript += "[" + msg.Timestamp.In(easternLocation).Format("2006-01-02 15:04:05") + "] " + roleLabel + ":\n"
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
