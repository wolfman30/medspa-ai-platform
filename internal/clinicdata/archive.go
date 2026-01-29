package clinicdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// S3Client interface for S3 operations (allows mocking in tests)
type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Archiver archives conversation data to S3 before deletion.
// Data is stored in JSONL format optimized for LLM training.
type Archiver struct {
	db     db
	s3     S3Client
	bucket string
	logger *logging.Logger
}

// ArchiverConfig holds configuration for the Archiver.
type ArchiverConfig struct {
	DB     db
	S3     S3Client
	Bucket string
	Logger *logging.Logger
}

// NewArchiver creates a new Archiver instance.
func NewArchiver(cfg ArchiverConfig) *Archiver {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &Archiver{
		db:     cfg.DB,
		s3:     cfg.S3,
		bucket: cfg.Bucket,
		logger: cfg.Logger,
	}
}

// ArchivedConversation represents a conversation archived for training.
// Format optimized for LLM fine-tuning (JSONL).
type ArchivedConversation struct {
	ConversationID string            `json:"conversation_id"`
	OrgID          string            `json:"org_id"`
	OrgName        string            `json:"org_name,omitempty"`
	Phone          string            `json:"phone_redacted"` // Partially redacted
	Channel        string            `json:"channel"`
	Messages       []ArchivedMessage `json:"messages"`
	Metadata       ArchiveMetadata   `json:"metadata"`
	ArchivedAt     time.Time         `json:"archived_at"`
	ArchiveReason  string            `json:"archive_reason"`
}

// ArchivedMessage represents a single message in the conversation.
type ArchivedMessage struct {
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status,omitempty"`
}

// ArchiveMetadata contains metadata about the conversation.
type ArchiveMetadata struct {
	LeadID           string   `json:"lead_id,omitempty"`
	MessageCount     int      `json:"message_count"`
	UserMessageCount int      `json:"user_message_count"`
	AIMessageCount   int      `json:"ai_message_count"`
	StartedAt        string   `json:"started_at,omitempty"`
	EndedAt          string   `json:"ended_at,omitempty"`
	Services         []string `json:"services_discussed,omitempty"`
	DepositCollected bool     `json:"deposit_collected"`
}

// ArchiveResult contains the result of an archive operation.
type ArchiveResult struct {
	ConversationsArchived int
	MessagesArchived      int
	S3Key                 string
	BytesWritten          int64
}

// ArchiveOrg archives all conversations for an organization before purge.
func (a *Archiver) ArchiveOrg(ctx context.Context, orgID, orgName string) (*ArchiveResult, error) {
	if a == nil || a.db == nil || a.s3 == nil || a.bucket == "" {
		return nil, fmt.Errorf("clinicdata: archiver not configured")
	}

	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, fmt.Errorf("clinicdata: missing orgID")
	}

	// Fetch all conversations for this org
	conversations, err := a.fetchOrgConversations(ctx, orgID, orgName)
	if err != nil {
		return nil, fmt.Errorf("clinicdata: fetch conversations: %w", err)
	}

	if len(conversations) == 0 {
		a.logger.Info("clinicdata: no conversations to archive", "org_id", orgID)
		return &ArchiveResult{}, nil
	}

	// Build JSONL content
	var buf bytes.Buffer
	totalMessages := 0
	for _, conv := range conversations {
		conv.ArchiveReason = "purge_org"
		conv.ArchivedAt = time.Now().UTC()

		line, err := json.Marshal(conv)
		if err != nil {
			a.logger.Warn("clinicdata: failed to marshal conversation", "error", err, "conversation_id", conv.ConversationID)
			continue
		}
		buf.Write(line)
		buf.WriteByte('\n')
		totalMessages += len(conv.Messages)
	}

	// Generate S3 key
	now := time.Now().UTC()
	s3Key := fmt.Sprintf("conversations/archive/%d/%02d/%02d/%s/bulk_%s.jsonl",
		now.Year(), now.Month(), now.Day(), orgID, now.Format("20060102T150405Z"))

	// Upload to S3
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(buf.Bytes()),
		ContentType: aws.String("application/x-ndjson"),
		Metadata: map[string]string{
			"org_id":             orgID,
			"org_name":           orgName,
			"archive_reason":     "purge_org",
			"conversation_count": fmt.Sprintf("%d", len(conversations)),
			"message_count":      fmt.Sprintf("%d", totalMessages),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("clinicdata: s3 upload failed: %w", err)
	}

	a.logger.Info("clinicdata: archived org conversations",
		"org_id", orgID,
		"conversations", len(conversations),
		"messages", totalMessages,
		"s3_key", s3Key,
	)

	return &ArchiveResult{
		ConversationsArchived: len(conversations),
		MessagesArchived:      totalMessages,
		S3Key:                 s3Key,
		BytesWritten:          int64(buf.Len()),
	}, nil
}

// ArchivePhone archives a single phone's conversation before purge.
func (a *Archiver) ArchivePhone(ctx context.Context, orgID, orgName, phone string) (*ArchiveResult, error) {
	if a == nil || a.db == nil || a.s3 == nil || a.bucket == "" {
		return nil, fmt.Errorf("clinicdata: archiver not configured")
	}

	orgID = strings.TrimSpace(orgID)
	phone = strings.TrimSpace(phone)
	if orgID == "" || phone == "" {
		return nil, fmt.Errorf("clinicdata: missing orgID or phone")
	}

	digits := sanitizeDigits(phone)
	if digits == "" {
		return nil, fmt.Errorf("clinicdata: invalid phone")
	}
	digits = normalizeUSDigits(digits)

	conversationID := fmt.Sprintf("sms:%s:%s", orgID, digits)

	// Fetch the conversation
	conv, err := a.fetchConversation(ctx, orgID, orgName, conversationID, digits)
	if err != nil {
		return nil, fmt.Errorf("clinicdata: fetch conversation: %w", err)
	}

	if conv == nil || len(conv.Messages) == 0 {
		a.logger.Info("clinicdata: no messages to archive", "conversation_id", conversationID)
		return &ArchiveResult{}, nil
	}

	conv.ArchiveReason = "purge_phone"
	conv.ArchivedAt = time.Now().UTC()

	// Build JSONL content (single line for single conversation)
	line, err := json.Marshal(conv)
	if err != nil {
		return nil, fmt.Errorf("clinicdata: marshal conversation: %w", err)
	}

	// Generate S3 key
	now := time.Now().UTC()
	s3Key := fmt.Sprintf("conversations/archive/%d/%02d/%02d/%s/%s_%s.jsonl",
		now.Year(), now.Month(), now.Day(), orgID, now.Format("20060102T150405Z"), digits)

	// Upload to S3
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(append(line, '\n')),
		ContentType: aws.String("application/x-ndjson"),
		Metadata: map[string]string{
			"org_id":          orgID,
			"conversation_id": conversationID,
			"archive_reason":  "purge_phone",
			"message_count":   fmt.Sprintf("%d", len(conv.Messages)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("clinicdata: s3 upload failed: %w", err)
	}

	a.logger.Info("clinicdata: archived phone conversation",
		"conversation_id", conversationID,
		"messages", len(conv.Messages),
		"s3_key", s3Key,
	)

	return &ArchiveResult{
		ConversationsArchived: 1,
		MessagesArchived:      len(conv.Messages),
		S3Key:                 s3Key,
		BytesWritten:          int64(len(line) + 1),
	}, nil
}

// fetchOrgConversations fetches all conversations for an org.
func (a *Archiver) fetchOrgConversations(ctx context.Context, orgID, orgName string) ([]ArchivedConversation, error) {
	conversationPattern := "sms:" + orgID + ":%"

	// Get all conversation IDs
	rows, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Rollback(ctx)

	convRows, err := rows.Query(ctx, `
		SELECT DISTINCT conversation_id
		FROM conversation_messages
		WHERE conversation_id LIKE $1
		ORDER BY conversation_id
	`, conversationPattern)
	if err != nil {
		return nil, err
	}
	defer convRows.Close()

	var conversationIDs []string
	for convRows.Next() {
		var id string
		if err := convRows.Scan(&id); err != nil {
			return nil, err
		}
		conversationIDs = append(conversationIDs, id)
	}

	// Fetch each conversation
	var conversations []ArchivedConversation
	for _, convID := range conversationIDs {
		// Extract phone digits from conversation ID (format: sms:orgID:digits)
		parts := strings.Split(convID, ":")
		digits := ""
		if len(parts) >= 3 {
			digits = parts[2]
		}

		conv, err := a.fetchConversationTx(ctx, rows, orgID, orgName, convID, digits)
		if err != nil {
			a.logger.Warn("clinicdata: failed to fetch conversation", "error", err, "conversation_id", convID)
			continue
		}
		if conv != nil && len(conv.Messages) > 0 {
			conversations = append(conversations, *conv)
		}
	}

	return conversations, nil
}

// fetchConversation fetches a single conversation with its messages.
func (a *Archiver) fetchConversation(ctx context.Context, orgID, orgName, conversationID, digits string) (*ArchivedConversation, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	return a.fetchConversationTx(ctx, tx, orgID, orgName, conversationID, digits)
}

// fetchConversationTx fetches a conversation within an existing transaction.
func (a *Archiver) fetchConversationTx(ctx context.Context, tx pgx.Tx, orgID, orgName, conversationID, digits string) (*ArchivedConversation, error) {
	// Fetch messages
	msgRows, err := tx.Query(ctx, `
		SELECT role, content, created_at, COALESCE(status, '')
		FROM conversation_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer msgRows.Close()

	var messages []ArchivedMessage
	var userCount, aiCount int
	var startedAt, endedAt time.Time

	for msgRows.Next() {
		var msg ArchivedMessage
		if err := msgRows.Scan(&msg.Role, &msg.Content, &msg.Timestamp, &msg.Status); err != nil {
			return nil, err
		}
		messages = append(messages, msg)

		if msg.Role == "user" {
			userCount++
		} else if msg.Role == "assistant" {
			aiCount++
		}

		if startedAt.IsZero() || msg.Timestamp.Before(startedAt) {
			startedAt = msg.Timestamp
		}
		if msg.Timestamp.After(endedAt) {
			endedAt = msg.Timestamp
		}
	}

	if len(messages) == 0 {
		return nil, nil
	}

	// Check if deposit was collected (look for payment in related lead)
	depositCollected := false
	var leadID string
	err = tx.QueryRow(ctx, `
		SELECT l.id::text, EXISTS(
			SELECT 1 FROM payments p WHERE p.lead_id = l.id AND p.status = 'completed'
		)
		FROM leads l
		WHERE l.org_id = $1 AND regexp_replace(l.phone, '\D', '', 'g') = $2
		LIMIT 1
	`, orgID, digits).Scan(&leadID, &depositCollected)
	if err != nil && err != pgx.ErrNoRows {
		// Non-fatal, just log
		a.logger.Warn("clinicdata: failed to check deposit status", "error", err)
	}

	// Redact phone for privacy (keep last 4 digits)
	redactedPhone := redactPhone(digits)

	return &ArchivedConversation{
		ConversationID: conversationID,
		OrgID:          orgID,
		OrgName:        orgName,
		Phone:          redactedPhone,
		Channel:        "sms",
		Messages:       messages,
		Metadata: ArchiveMetadata{
			LeadID:           leadID,
			MessageCount:     len(messages),
			UserMessageCount: userCount,
			AIMessageCount:   aiCount,
			StartedAt:        formatTime(startedAt),
			EndedAt:          formatTime(endedAt),
			DepositCollected: depositCollected,
		},
	}, nil
}

// redactPhone partially redacts a phone number for privacy.
// Input: "15551234567" -> Output: "+1-XXX-XXX-4567"
func redactPhone(digits string) string {
	if len(digits) < 4 {
		return "XXXX"
	}
	last4 := digits[len(digits)-4:]
	if len(digits) >= 11 {
		return fmt.Sprintf("+%s-XXX-XXX-%s", digits[:1], last4)
	}
	return fmt.Sprintf("XXX-XXX-%s", last4)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
