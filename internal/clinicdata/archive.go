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
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pgx/v5"
)

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

	result, err := a.uploadArchive(ctx, &buf, s3Key, map[string]string{
		"org_id":             orgID,
		"org_name":           orgName,
		"archive_reason":     "purge_org",
		"conversation_count": fmt.Sprintf("%d", len(conversations)),
		"message_count":      fmt.Sprintf("%d", totalMessages),
		"pii_redacted":       "true",
	})
	if err != nil {
		return nil, err
	}

	result.ConversationsArchived = len(conversations)
	result.MessagesArchived = totalMessages

	a.logger.Info("clinicdata: archived org conversations",
		"org_id", orgID,
		"conversations", len(conversations),
		"messages", totalMessages,
		"s3_key", s3Key,
		"encrypted", result.Encrypted,
	)

	return result, nil
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
	s3Key := fmt.Sprintf("conversations/archive/%d/%02d/%02d/%s/%s_conversation.jsonl",
		now.Year(), now.Month(), now.Day(), orgID, now.Format("20060102T150405Z"))

	var buf bytes.Buffer
	buf.Write(line)
	buf.WriteByte('\n')

	result, err := a.uploadArchive(ctx, &buf, s3Key, map[string]string{
		"org_id":         orgID,
		"archive_reason": "purge_phone",
		"message_count":  fmt.Sprintf("%d", len(conv.Messages)),
		"pii_redacted":   "true",
	})
	if err != nil {
		return nil, err
	}

	result.ConversationsArchived = 1
	result.MessagesArchived = len(conv.Messages)

	a.logger.Info("clinicdata: archived phone conversation",
		"conversation_id", conversationID,
		"messages", len(conv.Messages),
		"s3_key", s3Key,
		"encrypted", result.Encrypted,
	)

	return result, nil
}

// uploadArchive uploads JSONL data to S3 with optional SSE-KMS encryption.
func (a *Archiver) uploadArchive(ctx context.Context, buf *bytes.Buffer, s3Key string, metadata map[string]string) (*ArchiveResult, error) {
	putInput := &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(buf.Bytes()),
		ContentType: aws.String("application/x-ndjson"),
		Metadata:    metadata,
	}

	encrypted := false
	if a.kmsKeyID != "" {
		putInput.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		putInput.SSEKMSKeyId = aws.String(a.kmsKeyID)
		encrypted = true
	}

	if _, err := a.s3.PutObject(ctx, putInput); err != nil {
		return nil, fmt.Errorf("clinicdata: s3 upload failed: %w", err)
	}

	return &ArchiveResult{
		S3Key:        s3Key,
		BytesWritten: int64(buf.Len()),
		Encrypted:    encrypted,
		KMSKeyID:     a.kmsKeyID,
	}, nil
}

// fetchOrgConversations fetches all conversations for an org.
func (a *Archiver) fetchOrgConversations(ctx context.Context, orgID, orgName string) ([]ArchivedConversation, error) {
	conversationPattern := "sms:" + orgID + ":%"

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchOrgConversations: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	convRows, err := tx.Query(ctx, `
		SELECT DISTINCT conversation_id
		FROM conversation_messages
		WHERE conversation_id LIKE $1
		ORDER BY conversation_id
	`, conversationPattern)
	if err != nil {
		return nil, fmt.Errorf("fetchOrgConversations: query conversation IDs: %w", err)
	}
	defer convRows.Close()

	var conversationIDs []string
	for convRows.Next() {
		var id string
		if err := convRows.Scan(&id); err != nil {
			return nil, fmt.Errorf("fetchOrgConversations: scan conversation ID: %w", err)
		}
		conversationIDs = append(conversationIDs, id)
	}

	var conversations []ArchivedConversation
	for _, convID := range conversationIDs {
		// Extract phone digits from conversation ID (format: sms:orgID:digits)
		parts := strings.Split(convID, ":")
		digits := ""
		if len(parts) >= 3 {
			digits = parts[2]
		}

		conv, err := a.fetchConversationTx(ctx, tx, orgID, orgName, convID, digits)
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
		return nil, fmt.Errorf("fetchConversation: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	return a.fetchConversationTx(ctx, tx, orgID, orgName, conversationID, digits)
}

// fetchConversationTx fetches a conversation within an existing transaction.
func (a *Archiver) fetchConversationTx(ctx context.Context, tx pgx.Tx, orgID, orgName, conversationID, digits string) (*ArchivedConversation, error) {
	// Get the lead's name for redaction (if available)
	var leadName string
	depositCollected := false

	err := tx.QueryRow(ctx, `
		SELECT COALESCE(l.name, ''), EXISTS(
			SELECT 1 FROM payments p WHERE p.lead_id = l.id AND p.status = 'completed'
		)
		FROM leads l
		WHERE l.org_id = $1 AND regexp_replace(l.phone, '\D', '', 'g') = $2
		LIMIT 1
	`, orgID, digits).Scan(&leadName, &depositCollected)
	if err != nil && err != pgx.ErrNoRows {
		a.logger.Warn("clinicdata: failed to fetch lead info", "error", err)
	}

	knownNames := extractNames(leadName)

	// Fetch messages with delivery status for training data
	msgRows, err := tx.Query(ctx, `
		SELECT role, content, created_at, COALESCE(status, ''), COALESCE(error_reason, '')
		FROM conversation_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("fetchConversationTx: query messages: %w", err)
	}
	defer msgRows.Close()

	var messages []ArchivedMessage
	var userCount, aiCount int
	var startedAt, endedAt time.Time

	for msgRows.Next() {
		var msg ArchivedMessage
		var rawContent string
		if err := msgRows.Scan(&msg.Role, &rawContent, &msg.Timestamp, &msg.Status, &msg.ErrorReason); err != nil {
			return nil, fmt.Errorf("fetchConversationTx: scan message: %w", err)
		}

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
		Phone:          "[PHONE]",
		Channel:        "sms",
		Messages:       messages,
		Metadata: ArchiveMetadata{
			MessageCount:     len(messages),
			UserMessageCount: userCount,
			AIMessageCount:   aiCount,
			StartedAt:        formatTime(startedAt),
			EndedAt:          formatTime(endedAt),
			DepositCollected: depositCollected,
		},
	}, nil
}
