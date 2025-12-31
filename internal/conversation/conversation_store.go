package conversation

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ConversationStore persists conversations and messages to PostgreSQL for long-term history.
type ConversationStore struct {
	db *sql.DB
}

// NewConversationStore creates a new conversation store.
func NewConversationStore(db *sql.DB) *ConversationStore {
	if db == nil {
		return nil
	}
	return &ConversationStore{db: db}
}

// ConversationRecord represents a conversation in the database.
type ConversationRecord struct {
	ID                   uuid.UUID
	ConversationID       string
	OrgID                string
	LeadID               *uuid.UUID
	Phone                string
	Status               string
	Channel              string
	MessageCount         int
	CustomerMessageCount int
	AIMessageCount       int
	StartedAt            time.Time
	LastMessageAt        *time.Time
	EndedAt              *time.Time
}

// MessageRecord represents a message in the database.
type MessageRecord struct {
	ID                uuid.UUID
	ConversationID    string
	Role              string
	Content           string
	FromPhone         string
	ToPhone           string
	ProviderMessageID string
	Status            string
	ErrorReason       string
	CreatedAt         time.Time
}

// parseConversationID extracts orgID and phone from "sms:{orgID}:{phone}" format.
func parseConversationID(conversationID string) (orgID, phone string, ok bool) {
	parts := strings.Split(conversationID, ":")
	if len(parts) != 3 || parts[0] != "sms" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// EnsureConversation creates or updates a conversation record.
// Returns the conversation UUID.
func (s *ConversationStore) EnsureConversation(ctx context.Context, conversationID string) (uuid.UUID, error) {
	if s == nil || s.db == nil {
		return uuid.Nil, nil
	}

	orgID, phone, ok := parseConversationID(conversationID)
	if !ok {
		return uuid.Nil, fmt.Errorf("conversation: invalid conversation_id format: %s", conversationID)
	}

	// Try to get existing conversation
	var existingID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM conversations WHERE conversation_id = $1`,
		conversationID,
	).Scan(&existingID)

	if err == nil {
		// Update last activity
		s.db.ExecContext(ctx,
			`UPDATE conversations SET updated_at = $1 WHERE id = $2`,
			time.Now(), existingID,
		)
		return existingID, nil
	}

	if err != sql.ErrNoRows {
		return uuid.Nil, fmt.Errorf("conversation: failed to check existing: %w", err)
	}

	// Create new conversation
	newID := uuid.New()
	now := time.Now()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO conversations (
			id, conversation_id, org_id, phone, status, channel,
			message_count, customer_message_count, ai_message_count,
			started_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, newID, conversationID, orgID, phone, "active", "sms",
		0, 0, 0, now, now, now,
	)

	if err != nil {
		// Handle race condition - another process may have created it
		if strings.Contains(err.Error(), "duplicate key") {
			return s.EnsureConversation(ctx, conversationID)
		}
		return uuid.Nil, fmt.Errorf("conversation: failed to create: %w", err)
	}

	return newID, nil
}

// AppendMessage persists a message and updates conversation counters.
func (s *ConversationStore) AppendMessage(ctx context.Context, conversationID string, msg SMSTranscriptMessage) error {
	if s == nil || s.db == nil {
		return nil
	}

	// Ensure conversation exists
	_, err := s.EnsureConversation(ctx, conversationID)
	if err != nil {
		return err
	}

	// Insert message
	msgID := uuid.New()
	if msg.ID != "" {
		if parsed, parseErr := uuid.Parse(msg.ID); parseErr == nil {
			msgID = parsed
		}
	}

	timestamp := msg.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO conversation_messages (
			id, conversation_id, role, content, from_phone, to_phone, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO NOTHING
	`, msgID, conversationID, msg.Role, msg.Body, msg.From, msg.To, timestamp)

	if err != nil {
		return fmt.Errorf("conversation: failed to insert message: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("conversation: failed to read insert result: %w", err)
	}
	if rowsAffected == 0 {
		return nil
	}

	// Update conversation counters
	counterColumn := "message_count"
	if msg.Role == "user" {
		counterColumn = "customer_message_count"
	} else if msg.Role == "assistant" {
		counterColumn = "ai_message_count"
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE conversations SET
			message_count = message_count + 1,
			%s = %s + 1,
			last_message_at = $1,
			updated_at = $1
		WHERE conversation_id = $2
	`, counterColumn, counterColumn), timestamp, conversationID)

	if err != nil {
		return fmt.Errorf("conversation: failed to update counters: %w", err)
	}

	return nil
}

// EndConversation marks a conversation as ended.
func (s *ConversationStore) EndConversation(ctx context.Context, conversationID string) error {
	if s == nil || s.db == nil {
		return nil
	}

	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		UPDATE conversations SET
			status = 'ended',
			ended_at = $1,
			updated_at = $1
		WHERE conversation_id = $2 AND ended_at IS NULL
	`, now, conversationID)

	return err
}

// GetConversation retrieves a conversation by its ID.
func (s *ConversationStore) GetConversation(ctx context.Context, conversationID string) (*ConversationRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	var conv ConversationRecord
	var leadID sql.NullString
	var lastMessageAt, endedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, `
		SELECT id, conversation_id, org_id, lead_id, phone, status, channel,
			   message_count, customer_message_count, ai_message_count,
			   started_at, last_message_at, ended_at
		FROM conversations
		WHERE conversation_id = $1
	`, conversationID).Scan(
		&conv.ID, &conv.ConversationID, &conv.OrgID, &leadID, &conv.Phone,
		&conv.Status, &conv.Channel, &conv.MessageCount, &conv.CustomerMessageCount,
		&conv.AIMessageCount, &conv.StartedAt, &lastMessageAt, &endedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("conversation: failed to get: %w", err)
	}

	if leadID.Valid {
		if parsed, parseErr := uuid.Parse(leadID.String); parseErr == nil {
			conv.LeadID = &parsed
		}
	}
	if lastMessageAt.Valid {
		conv.LastMessageAt = &lastMessageAt.Time
	}
	if endedAt.Valid {
		conv.EndedAt = &endedAt.Time
	}

	return &conv, nil
}

// GetMessages retrieves messages for a conversation.
func (s *ConversationStore) GetMessages(ctx context.Context, conversationID string, limit int) ([]MessageRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	query := `
		SELECT id, conversation_id, role, content, from_phone, to_phone,
			   COALESCE(provider_message_id, ''), COALESCE(status, 'delivered'),
			   COALESCE(error_reason, ''), created_at
		FROM conversation_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`
	args := []any{conversationID}

	if limit > 0 {
		query += " LIMIT $2"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("conversation: failed to get messages: %w", err)
	}
	defer rows.Close()

	var messages []MessageRecord
	for rows.Next() {
		var msg MessageRecord
		err := rows.Scan(
			&msg.ID, &msg.ConversationID, &msg.Role, &msg.Content,
			&msg.FromPhone, &msg.ToPhone, &msg.ProviderMessageID,
			&msg.Status, &msg.ErrorReason, &msg.CreatedAt,
		)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// LinkLead associates a lead with a conversation.
func (s *ConversationStore) LinkLead(ctx context.Context, conversationID string, leadID uuid.UUID) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE conversations SET lead_id = $1, updated_at = $2
		WHERE conversation_id = $3
	`, leadID, time.Now(), conversationID)

	return err
}

// HasAssistantMessage returns true if the conversation has any assistant messages stored.
func (s *ConversationStore) HasAssistantMessage(ctx context.Context, conversationID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(conversationID) == "" {
		return false, nil
	}

	var exists int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM conversation_messages
		WHERE conversation_id = $1 AND role = 'assistant'
		LIMIT 1
	`, conversationID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("conversation: check assistant messages: %w", err)
	}
	return true, nil
}
