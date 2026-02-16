// Package compliance provides healthcare regulatory compliance features.
package compliance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AuditEventType represents the type of compliance event.
type AuditEventType string

const (
	// EventMedicalAdviceRefused is logged when AI refuses a medical advice request.
	EventMedicalAdviceRefused AuditEventType = "compliance.medical_advice_refused"
	// EventPHIDetected is logged when PHI is detected in conversation.
	EventPHIDetected AuditEventType = "compliance.phi_detected"
	// EventDisclaimerSent is logged when a disclaimer is added to a message.
	EventDisclaimerSent AuditEventType = "compliance.disclaimer_sent"
	// EventSupervisorReview is logged when supervisor mode modifies a response.
	EventSupervisorReview AuditEventType = "compliance.supervisor_review"
	// EventResponseModified is logged when AI response is edited for safety.
	EventResponseModified AuditEventType = "compliance.response_modified"
	// EventKnowledgeRead is logged when clinic knowledge is read.
	EventKnowledgeRead AuditEventType = "compliance.knowledge_read"
	// EventKnowledgeUpdated is logged when clinic knowledge is updated.
	EventKnowledgeUpdated AuditEventType = "compliance.knowledge_updated"
	// EventPromptInjection is logged when a prompt injection attempt is detected.
	EventPromptInjection AuditEventType = "security.prompt_injection"
)

// AuditEvent represents an immutable compliance audit record.
type AuditEvent struct {
	ID             string          `json:"id"`
	EventType      AuditEventType  `json:"event_type"`
	OrgID          string          `json:"org_id"`
	ConversationID string          `json:"conversation_id,omitempty"`
	LeadID         string          `json:"lead_id,omitempty"`
	UserMessage    string          `json:"user_message,omitempty"`
	AIResponse     string          `json:"ai_response,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// AuditDetails contains event-specific details.
type AuditDetails struct {
	// For medical advice refused
	DetectedKeywords []string `json:"detected_keywords,omitempty"`
	RefusalReason    string   `json:"refusal_reason,omitempty"`

	// For PHI detected
	PHIType     string `json:"phi_type,omitempty"`
	PHIRedacted bool   `json:"phi_redacted,omitempty"`

	// For disclaimer sent
	DisclaimerLevel string `json:"disclaimer_level,omitempty"`
	DisclaimerText  string `json:"disclaimer_text,omitempty"`

	// For prompt injection detected
	InjectionReasons []string `json:"injection_reasons,omitempty"`

	// For supervisor review
	SupervisorMode     string `json:"supervisor_mode,omitempty"`
	OriginalResponse   string `json:"original_response,omitempty"`
	ModifiedResponse   string `json:"modified_response,omitempty"`
	ModificationReason string `json:"modification_reason,omitempty"`
}

// AuditService handles compliance audit logging.
type AuditService struct {
	db *sql.DB
}

// NewAuditService creates a new audit service.
func NewAuditService(db *sql.DB) *AuditService {
	return &AuditService{db: db}
}

// LogEvent records a compliance audit event.
func (s *AuditService) LogEvent(ctx context.Context, event AuditEvent) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	query := `
		INSERT INTO compliance_audit_events (
			id, event_type, org_id, conversation_id, lead_id,
			user_message, ai_response, details, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := s.db.ExecContext(ctx, query,
		event.ID,
		event.EventType,
		event.OrgID,
		nullString(event.ConversationID),
		nullString(event.LeadID),
		nullString(event.UserMessage),
		nullString(event.AIResponse),
		event.Details,
		event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("compliance: failed to log audit event: %w", err)
	}

	return nil
}

// LogMedicalAdviceRefused logs when AI refuses to provide medical advice.
func (s *AuditService) LogMedicalAdviceRefused(ctx context.Context, orgID, conversationID, leadID, userMessage string, keywords []string) error {
	details := AuditDetails{
		DetectedKeywords: keywords,
		RefusalReason:    "Detected medical advice request",
	}
	detailsJSON, _ := json.Marshal(details)

	return s.LogEvent(ctx, AuditEvent{
		EventType:      EventMedicalAdviceRefused,
		OrgID:          orgID,
		ConversationID: conversationID,
		LeadID:         leadID,
		UserMessage:    userMessage,
		Details:        detailsJSON,
	})
}

// LogPHIDetected logs when PHI is detected in a message.
func (s *AuditService) LogPHIDetected(ctx context.Context, orgID, conversationID, leadID, userMessage, phiType string) error {
	details := AuditDetails{
		PHIType:     phiType,
		PHIRedacted: true,
	}
	detailsJSON, _ := json.Marshal(details)

	return s.LogEvent(ctx, AuditEvent{
		EventType:      EventPHIDetected,
		OrgID:          orgID,
		ConversationID: conversationID,
		LeadID:         leadID,
		UserMessage:    "[REDACTED]", // Don't store PHI in audit log
		Details:        detailsJSON,
	})
}

// LogPromptInjection logs when a prompt injection attempt is detected and blocked.
func (s *AuditService) LogPromptInjection(ctx context.Context, orgID, conversationID, leadID string, reasons []string) error {
	details := AuditDetails{
		InjectionReasons: reasons,
	}
	detailsJSON, _ := json.Marshal(details)

	return s.LogEvent(ctx, AuditEvent{
		EventType:      EventPromptInjection,
		OrgID:          orgID,
		ConversationID: conversationID,
		LeadID:         leadID,
		UserMessage:    "[BLOCKED]", // Don't store injection payload
		Details:        detailsJSON,
	})
}

// LogDisclaimerSent logs when a disclaimer is added to a message.
func (s *AuditService) LogDisclaimerSent(ctx context.Context, orgID, conversationID, leadID, level, disclaimerText string) error {
	details := AuditDetails{
		DisclaimerLevel: level,
		DisclaimerText:  disclaimerText,
	}
	detailsJSON, _ := json.Marshal(details)

	return s.LogEvent(ctx, AuditEvent{
		EventType:      EventDisclaimerSent,
		OrgID:          orgID,
		ConversationID: conversationID,
		LeadID:         leadID,
		Details:        detailsJSON,
	})
}

// LogSupervisorReview logs when supervisor mode modifies a response.
func (s *AuditService) LogSupervisorReview(ctx context.Context, orgID, conversationID, leadID, mode, original, modified, reason string) error {
	details := AuditDetails{
		SupervisorMode:     mode,
		OriginalResponse:   original,
		ModifiedResponse:   modified,
		ModificationReason: reason,
	}
	detailsJSON, _ := json.Marshal(details)

	return s.LogEvent(ctx, AuditEvent{
		EventType:      EventSupervisorReview,
		OrgID:          orgID,
		ConversationID: conversationID,
		LeadID:         leadID,
		AIResponse:     modified,
		Details:        detailsJSON,
	})
}

// QueryEvents retrieves audit events with filters.
func (s *AuditService) QueryEvents(ctx context.Context, filter AuditFilter) ([]AuditEvent, error) {
	query := `
		SELECT id, event_type, org_id, conversation_id, lead_id,
			   user_message, ai_response, details, created_at
		FROM compliance_audit_events
		WHERE org_id = $1
	`
	args := []interface{}{filter.OrgID}
	argIdx := 2

	if filter.ConversationID != "" {
		query += fmt.Sprintf(" AND conversation_id = $%d", argIdx)
		args = append(args, filter.ConversationID)
		argIdx++
	}
	if filter.EventType != "" {
		query += fmt.Sprintf(" AND event_type = $%d", argIdx)
		args = append(args, filter.EventType)
		argIdx++
	}
	if !filter.StartTime.IsZero() {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, filter.StartTime)
		argIdx++
	}
	if !filter.EndTime.IsZero() {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, filter.EndTime)
		argIdx++
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("compliance: failed to query audit events: %w", err)
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var convID, leadID, userMsg, aiResp sql.NullString
		err := rows.Scan(
			&e.ID, &e.EventType, &e.OrgID, &convID, &leadID,
			&userMsg, &aiResp, &e.Details, &e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("compliance: failed to scan audit event: %w", err)
		}
		e.ConversationID = convID.String
		e.LeadID = leadID.String
		e.UserMessage = userMsg.String
		e.AIResponse = aiResp.String
		events = append(events, e)
	}

	return events, nil
}

// AuditFilter specifies criteria for querying audit events.
type AuditFilter struct {
	OrgID          string
	ConversationID string
	EventType      AuditEventType
	StartTime      time.Time
	EndTime        time.Time
	Limit          int
	Offset         int
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
