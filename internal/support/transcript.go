package support

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var transcriptTracer = otel.Tracer("medspa/transcript")

// TranscriptMessage represents a single message in a conversation.
type TranscriptMessage struct {
	ID        uuid.UUID
	Role      string // "customer", "assistant", "system"
	Content   string
	Timestamp time.Time
	Metadata  map[string]any
}

// ConversationTranscript represents a full conversation transcript.
type ConversationTranscript struct {
	ConversationID string
	OrgID          string
	LeadID         string
	CustomerPhone  string
	CustomerName   string
	StartedAt      time.Time
	EndedAt        *time.Time
	Messages       []TranscriptMessage
	Summary        string
	Tags           []string
}

// TranscriptService handles conversation transcript operations.
type TranscriptService struct {
	db       *sql.DB
	logger   *logging.Logger
	notifier NotificationChannel
}

// NewTranscriptService creates a new transcript service.
func NewTranscriptService(db *sql.DB, notifier NotificationChannel, logger *logging.Logger) *TranscriptService {
	if logger == nil {
		logger = logging.Default()
	}
	return &TranscriptService{
		db:       db,
		logger:   logger,
		notifier: notifier,
	}
}

// GetTranscript retrieves a conversation transcript.
func (s *TranscriptService) GetTranscript(ctx context.Context, conversationID string) (*ConversationTranscript, error) {
	ctx, span := transcriptTracer.Start(ctx, "transcript.get")
	defer span.End()
	span.SetAttributes(attribute.String("conversation.id", conversationID))

	// Get conversation metadata
	var transcript ConversationTranscript
	var leadID, customerName sql.NullString
	var endedAt sql.NullTime

	metaQuery := `
		SELECT c.conversation_id, c.org_id, c.lead_id, c.phone, l.name, c.started_at, c.ended_at
		FROM conversations c
		LEFT JOIN leads l ON c.lead_id = l.id
		WHERE c.conversation_id = $1
	`
	err := s.db.QueryRowContext(ctx, metaQuery, conversationID).Scan(
		&transcript.ConversationID,
		&transcript.OrgID,
		&leadID,
		&transcript.CustomerPhone,
		&customerName,
		&transcript.StartedAt,
		&endedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("support: get transcript metadata: %w", err)
	}

	transcript.LeadID = leadID.String
	transcript.CustomerName = customerName.String
	if endedAt.Valid {
		transcript.EndedAt = &endedAt.Time
	}

	// Get messages
	msgQuery := `
		SELECT id, role, content, created_at
		FROM conversation_messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, msgQuery, conversationID)
	if err != nil {
		return nil, fmt.Errorf("support: get transcript messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var msg TranscriptMessage
		err := rows.Scan(&msg.ID, &msg.Role, &msg.Content, &msg.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("support: scan message: %w", err)
		}
		transcript.Messages = append(transcript.Messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &transcript, nil
}

// GetRecentTranscripts retrieves recent conversation transcripts for an org.
func (s *TranscriptService) GetRecentTranscripts(ctx context.Context, orgID string, limit int) ([]*ConversationTranscript, error) {
	query := `
		SELECT DISTINCT c.conversation_id
		FROM conversations c
		WHERE c.org_id = $1
		ORDER BY c.started_at DESC
		LIMIT $2
	`
	rows, err := s.db.QueryContext(ctx, query, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcripts []*ConversationTranscript
	for rows.Next() {
		var convID string
		if err := rows.Scan(&convID); err != nil {
			return nil, err
		}
		transcript, err := s.GetTranscript(ctx, convID)
		if err != nil {
			s.logger.Error("failed to get transcript", "error", err, "conversation_id", convID)
			continue
		}
		transcripts = append(transcripts, transcript)
	}

	return transcripts, rows.Err()
}

// ForwardTranscript sends a conversation transcript to staff.
func (s *TranscriptService) ForwardTranscript(ctx context.Context, conversationID, reason string) error {
	ctx, span := transcriptTracer.Start(ctx, "transcript.forward")
	defer span.End()
	span.SetAttributes(attribute.String("conversation.id", conversationID))

	transcript, err := s.GetTranscript(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("support: get transcript for forwarding: %w", err)
	}

	// Get staff contact
	var staffEmail string
	err = s.db.QueryRowContext(ctx,
		`SELECT contact_email FROM organizations WHERE id = $1`,
		transcript.OrgID,
	).Scan(&staffEmail)
	if err != nil || staffEmail == "" {
		return fmt.Errorf("support: no staff email configured")
	}

	// Format and send
	subject := fmt.Sprintf("Conversation Transcript: %s - %s", transcript.CustomerPhone, reason)
	body := s.formatTranscriptEmail(transcript, reason)

	if s.notifier != nil {
		if err := s.notifier.SendEmail(ctx, staffEmail, subject, body); err != nil {
			return fmt.Errorf("support: send transcript email: %w", err)
		}
	}

	s.logger.Info("transcript forwarded",
		"conversation_id", conversationID,
		"reason", reason,
		"message_count", len(transcript.Messages),
	)

	return nil
}

// FormatPlainText formats a transcript as plain text.
func (s *TranscriptService) FormatPlainText(transcript *ConversationTranscript) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Conversation ID: %s\n", transcript.ConversationID))
	sb.WriteString(fmt.Sprintf("Customer: %s", transcript.CustomerPhone))
	if transcript.CustomerName != "" {
		sb.WriteString(fmt.Sprintf(" (%s)", transcript.CustomerName))
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Started: %s\n", transcript.StartedAt.Format(time.RFC1123)))
	if transcript.EndedAt != nil {
		sb.WriteString(fmt.Sprintf("Ended: %s\n", transcript.EndedAt.Format(time.RFC1123)))
	}
	sb.WriteString("\n--- Messages ---\n\n")

	for _, msg := range transcript.Messages {
		role := strings.ToUpper(msg.Role)
		if role == "ASSISTANT" {
			role = "AI"
		}
		sb.WriteString(fmt.Sprintf("[%s] %s:\n%s\n\n",
			msg.Timestamp.Format("15:04:05"),
			role,
			msg.Content,
		))
	}

	return sb.String()
}

// GenerateSummary creates a brief summary of the conversation.
func (s *TranscriptService) GenerateSummary(transcript *ConversationTranscript) string {
	if len(transcript.Messages) == 0 {
		return "No messages in conversation"
	}

	// Count messages by role
	customerMsgs := 0
	aiMsgs := 0
	for _, msg := range transcript.Messages {
		switch msg.Role {
		case "customer", "user":
			customerMsgs++
		case "assistant":
			aiMsgs++
		}
	}

	duration := time.Since(transcript.StartedAt)
	if transcript.EndedAt != nil {
		duration = transcript.EndedAt.Sub(transcript.StartedAt)
	}

	// Get first customer message as topic indicator
	topic := ""
	for _, msg := range transcript.Messages {
		if msg.Role == "customer" || msg.Role == "user" {
			topic = msg.Content
			if len(topic) > 100 {
				topic = topic[:100] + "..."
			}
			break
		}
	}

	return fmt.Sprintf("Duration: %s | %d customer messages, %d AI responses | Topic: %s",
		duration.Round(time.Minute),
		customerMsgs,
		aiMsgs,
		topic,
	)
}

func (s *TranscriptService) formatTranscriptEmail(transcript *ConversationTranscript, reason string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Reason for forwarding: %s\n\n", reason))
	sb.WriteString("=== Conversation Details ===\n\n")
	sb.WriteString(s.FormatPlainText(transcript))

	if transcript.Summary != "" {
		sb.WriteString("\n=== Summary ===\n")
		sb.WriteString(transcript.Summary)
		sb.WriteString("\n")
	}

	return sb.String()
}

// GetTranscriptsByLead retrieves all conversation transcripts for a lead.
func (s *TranscriptService) GetTranscriptsByLead(ctx context.Context, leadID string) ([]*ConversationTranscript, error) {
	query := `
		SELECT conversation_id FROM conversations WHERE lead_id = $1 ORDER BY started_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query, leadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcripts []*ConversationTranscript
	for rows.Next() {
		var convID string
		if err := rows.Scan(&convID); err != nil {
			return nil, err
		}
		transcript, err := s.GetTranscript(ctx, convID)
		if err != nil {
			continue
		}
		transcripts = append(transcripts, transcript)
	}

	return transcripts, rows.Err()
}

// SearchTranscripts searches for conversations containing specific text.
func (s *TranscriptService) SearchTranscripts(ctx context.Context, orgID, searchText string, limit int) ([]*ConversationTranscript, error) {
	query := `
		SELECT DISTINCT c.conversation_id
		FROM conversations c
		JOIN conversation_messages m ON c.conversation_id = m.conversation_id
		WHERE c.org_id = $1 AND m.content ILIKE $2
		ORDER BY c.started_at DESC
		LIMIT $3
	`
	searchPattern := "%" + searchText + "%"

	rows, err := s.db.QueryContext(ctx, query, orgID, searchPattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcripts []*ConversationTranscript
	for rows.Next() {
		var convID string
		if err := rows.Scan(&convID); err != nil {
			return nil, err
		}
		transcript, err := s.GetTranscript(ctx, convID)
		if err != nil {
			continue
		}
		transcripts = append(transcripts, transcript)
	}

	return transcripts, rows.Err()
}
