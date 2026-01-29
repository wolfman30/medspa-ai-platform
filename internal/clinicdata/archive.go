package clinicdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pgx/v5"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// S3Client interface for S3 operations (allows mocking in tests)
type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Archiver archives conversation data to S3 before deletion.
// Data is stored in JSONL format optimized for LLM training.
// PII (names, phones) is redacted for HIPAA compliance.
type Archiver struct {
	db       db
	s3       S3Client
	bucket   string
	kmsKeyID string // Optional: KMS key for SSE-KMS encryption
	logger   *logging.Logger
}

// ArchiverConfig holds configuration for the Archiver.
type ArchiverConfig struct {
	DB       db
	S3       S3Client
	Bucket   string
	KMSKeyID string // Optional: KMS key ID for server-side encryption (SSE-KMS)
	Logger   *logging.Logger
}

// NewArchiver creates a new Archiver instance.
func NewArchiver(cfg ArchiverConfig) *Archiver {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &Archiver{
		db:       cfg.DB,
		s3:       cfg.S3,
		bucket:   cfg.Bucket,
		kmsKeyID: cfg.KMSKeyID,
		logger:   cfg.Logger,
	}
}

// ArchivedConversation represents a conversation archived for training.
// Format optimized for LLM fine-tuning (JSONL).
// All PII (names, phones) is redacted.
type ArchivedConversation struct {
	ConversationID string            `json:"conversation_id"`
	OrgID          string            `json:"org_id"`
	OrgName        string            `json:"org_name,omitempty"`
	Phone          string            `json:"phone_redacted"` // Fully redacted as [PHONE]
	Channel        string            `json:"channel"`
	Messages       []ArchivedMessage `json:"messages"`
	Metadata       ArchiveMetadata   `json:"metadata"`
	ArchivedAt     time.Time         `json:"archived_at"`
	ArchiveReason  string            `json:"archive_reason"`
}

// ArchivedMessage represents a single message in the conversation.
type ArchivedMessage struct {
	Role        string    `json:"role"` // "user" or "assistant"
	Content     string    `json:"content"`
	Timestamp   time.Time `json:"timestamp"`
	Status      string    `json:"status,omitempty"`       // e.g., "delivered", "sent", "failed"
	ErrorReason string    `json:"error_reason,omitempty"` // e.g., "spam", "carrier_rejected" - for training LLM on blocked messages
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
	Encrypted             bool   // True if SSE-KMS encryption was applied
	KMSKeyID              string // KMS key used for encryption (if any)
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

	// Build S3 PutObject input with optional SSE-KMS encryption
	putInput := &s3.PutObjectInput{
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
			"pii_redacted":       "true",
		},
	}

	// Apply SSE-KMS encryption if KMS key is configured
	encrypted := false
	if a.kmsKeyID != "" {
		putInput.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		putInput.SSEKMSKeyId = aws.String(a.kmsKeyID)
		encrypted = true
	}

	// Upload to S3
	_, err = a.s3.PutObject(ctx, putInput)
	if err != nil {
		return nil, fmt.Errorf("clinicdata: s3 upload failed: %w", err)
	}

	a.logger.Info("clinicdata: archived org conversations",
		"org_id", orgID,
		"conversations", len(conversations),
		"messages", totalMessages,
		"s3_key", s3Key,
		"encrypted", encrypted,
	)

	return &ArchiveResult{
		ConversationsArchived: len(conversations),
		MessagesArchived:      totalMessages,
		S3Key:                 s3Key,
		BytesWritten:          int64(buf.Len()),
		Encrypted:             encrypted,
		KMSKeyID:              a.kmsKeyID,
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

	// Generate S3 key (use [PHONE] placeholder instead of actual digits)
	now := time.Now().UTC()
	s3Key := fmt.Sprintf("conversations/archive/%d/%02d/%02d/%s/%s_conversation.jsonl",
		now.Year(), now.Month(), now.Day(), orgID, now.Format("20060102T150405Z"))

	// Build S3 PutObject input with optional SSE-KMS encryption
	putInput := &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(append(line, '\n')),
		ContentType: aws.String("application/x-ndjson"),
		Metadata: map[string]string{
			"org_id":         orgID,
			"archive_reason": "purge_phone",
			"message_count":  fmt.Sprintf("%d", len(conv.Messages)),
			"pii_redacted":   "true",
		},
	}

	// Apply SSE-KMS encryption if KMS key is configured
	encrypted := false
	if a.kmsKeyID != "" {
		putInput.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		putInput.SSEKMSKeyId = aws.String(a.kmsKeyID)
		encrypted = true
	}

	// Upload to S3
	_, err = a.s3.PutObject(ctx, putInput)
	if err != nil {
		return nil, fmt.Errorf("clinicdata: s3 upload failed: %w", err)
	}

	a.logger.Info("clinicdata: archived phone conversation",
		"conversation_id", conversationID,
		"messages", len(conv.Messages),
		"s3_key", s3Key,
		"encrypted", encrypted,
	)

	return &ArchiveResult{
		ConversationsArchived: 1,
		MessagesArchived:      len(conv.Messages),
		S3Key:                 s3Key,
		BytesWritten:          int64(len(line) + 1),
		Encrypted:             encrypted,
		KMSKeyID:              a.kmsKeyID,
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
	// First, get the lead's name for redaction (if available)
	var leadName string
	var leadID string
	depositCollected := false

	err := tx.QueryRow(ctx, `
		SELECT l.id::text, COALESCE(l.name, ''), EXISTS(
			SELECT 1 FROM payments p WHERE p.lead_id = l.id AND p.status = 'completed'
		)
		FROM leads l
		WHERE l.org_id = $1 AND regexp_replace(l.phone, '\D', '', 'g') = $2
		LIMIT 1
	`, orgID, digits).Scan(&leadID, &leadName, &depositCollected)
	if err != nil && err != pgx.ErrNoRows {
		a.logger.Warn("clinicdata: failed to fetch lead info", "error", err)
	}

	// Build list of known names for redaction
	knownNames := extractNames(leadName)

	// Fetch messages with delivery status for training data
	msgRows, err := tx.Query(ctx, `
		SELECT role, content, created_at, COALESCE(status, ''), COALESCE(error_reason, '')
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
		var rawContent string
		if err := msgRows.Scan(&msg.Role, &rawContent, &msg.Timestamp, &msg.Status, &msg.ErrorReason); err != nil {
			return nil, err
		}

		// Redact PII from message content
		msg.Content = redactPII(rawContent, knownNames)
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

	return &ArchivedConversation{
		ConversationID: redactConversationID(conversationID),
		OrgID:          orgID,
		OrgName:        orgName,
		Phone:          "[PHONE]", // Fully redacted
		Channel:        "sms",
		Messages:       messages,
		Metadata: ArchiveMetadata{
			LeadID:           "", // Don't include lead ID in archived data
			MessageCount:     len(messages),
			UserMessageCount: userCount,
			AIMessageCount:   aiCount,
			StartedAt:        formatTime(startedAt),
			EndedAt:          formatTime(endedAt),
			DepositCollected: depositCollected,
		},
	}, nil
}

// redactConversationID removes the phone number from the conversation ID.
// Input: "sms:org-uuid:15551234567" -> Output: "sms:org-uuid:[PHONE]"
func redactConversationID(convID string) string {
	parts := strings.Split(convID, ":")
	if len(parts) >= 3 {
		parts[2] = "[PHONE]"
		return strings.Join(parts, ":")
	}
	return convID
}

// extractNames splits a full name into individual name components for redaction.
func extractNames(fullName string) []string {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return nil
	}

	var names []string
	parts := strings.Fields(fullName)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		// Only include names that are at least 2 characters
		if len(part) >= 2 {
			names = append(names, part)
		}
	}
	return names
}

// Regex patterns for detecting names in text
var (
	// Patterns like "I'm Sarah", "I am John", "My name is Jane", "This is Mike", "call me Bob"
	nameIntroPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:i'?m|i am|my name is|this is|call me|it's|its)\s+([A-Z][a-z]+(?:\s+[A-Z][a-z]+)?)`),
		regexp.MustCompile(`(?i)\b([A-Z][a-z]+(?:\s+[A-Z][a-z]+)?)\s+here\b`),
	}

	// Phone number patterns (various formats)
	phonePatterns = []*regexp.Regexp{
		regexp.MustCompile(`\+?1?[-.\s]?\(?[0-9]{3}\)?[-.\s]?[0-9]{3}[-.\s]?[0-9]{4}`),
		regexp.MustCompile(`\b[0-9]{10,11}\b`),
	}

	// Email pattern
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
)

// redactPII removes personally identifiable information from text.
// Redacts: names (known and detected), phone numbers, emails.
func redactPII(text string, knownNames []string) string {
	if text == "" {
		return text
	}

	// Redact known names (from lead record)
	for _, name := range knownNames {
		if len(name) >= 2 {
			// Case-insensitive replacement
			pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
			text = pattern.ReplaceAllString(text, "[NAME]")
		}
	}

	// Detect and redact names from common introduction patterns
	for _, pattern := range nameIntroPatterns {
		text = pattern.ReplaceAllStringFunc(text, func(match string) string {
			// Find the name part and redact it
			submatches := pattern.FindStringSubmatch(match)
			if len(submatches) >= 2 {
				name := submatches[1]
				return strings.Replace(match, name, "[NAME]", 1)
			}
			return match
		})
	}

	// Redact phone numbers
	for _, pattern := range phonePatterns {
		text = pattern.ReplaceAllString(text, "[PHONE]")
	}

	// Redact email addresses
	text = emailPattern.ReplaceAllString(text, "[EMAIL]")

	return text
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
