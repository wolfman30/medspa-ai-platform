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

var escalationTracer = otel.Tracer("medspa/escalation")

// EscalationType represents the type of escalation.
type EscalationType string

const (
	EscalationComplaint       EscalationType = "COMPLAINT"
	EscalationDispute         EscalationType = "DISPUTE"
	EscalationRefundRequest   EscalationType = "REFUND_REQUEST"
	EscalationVelocityBlock   EscalationType = "VELOCITY_BLOCK"
	EscalationUnauthorized    EscalationType = "UNAUTHORIZED_CHARGE"
	EscalationMedicalConcern  EscalationType = "MEDICAL_CONCERN"
	EscalationCallbackOverdue EscalationType = "CALLBACK_OVERDUE"
)

// EscalationPriority represents the urgency level.
type EscalationPriority string

const (
	PriorityHigh   EscalationPriority = "HIGH"
	PriorityMedium EscalationPriority = "MEDIUM"
	PriorityLow    EscalationPriority = "LOW"
)

// EscalationStatus represents the status of an escalation.
type EscalationStatus string

const (
	StatusPending      EscalationStatus = "PENDING"
	StatusAcknowledged EscalationStatus = "ACKNOWLEDGED"
	StatusInProgress   EscalationStatus = "IN_PROGRESS"
	StatusResolved     EscalationStatus = "RESOLVED"
	StatusClosed       EscalationStatus = "CLOSED"
)

// Escalation represents a staff escalation record.
type Escalation struct {
	ID                uuid.UUID
	OrgID             string
	Type              EscalationType
	Priority          EscalationPriority
	Status            EscalationStatus
	CustomerPhone     string
	CustomerName      string
	LeadID            string
	ConversationID    string
	PaymentID         string
	AmountCents       int32
	Description       string
	RecommendedAction string
	TranscriptSummary string
	AcknowledgedAt    *time.Time
	AcknowledgedBy    string
	ResolvedAt        *time.Time
	ResolvedBy        string
	Resolution        string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// EscalationRequest contains details for creating an escalation.
type EscalationRequest struct {
	OrgID             string
	Type              EscalationType
	Priority          EscalationPriority
	CustomerPhone     string
	CustomerName      string
	LeadID            string
	ConversationID    string
	PaymentID         string
	AmountCents       int32
	Description       string
	RecommendedAction string
	TranscriptSummary string
}

// NotificationChannel represents a channel for sending notifications.
type NotificationChannel interface {
	SendSMS(ctx context.Context, phone, message string) error
	SendEmail(ctx context.Context, email, subject, body string) error
}

// EscalationService handles staff escalations.
type EscalationService struct {
	db       *sql.DB
	logger   *logging.Logger
	notifier NotificationChannel
	slaHours int // Hours before SLA alert
}

// NewEscalationService creates a new escalation service.
func NewEscalationService(db *sql.DB, notifier NotificationChannel, logger *logging.Logger) *EscalationService {
	if logger == nil {
		logger = logging.Default()
	}
	return &EscalationService{
		db:       db,
		logger:   logger,
		notifier: notifier,
		slaHours: 4, // Default 4 hour SLA
	}
}

// CreateEscalation creates a new escalation and notifies staff.
func (s *EscalationService) CreateEscalation(ctx context.Context, req EscalationRequest) (*Escalation, error) {
	ctx, span := escalationTracer.Start(ctx, "escalation.create")
	defer span.End()
	span.SetAttributes(
		attribute.String("escalation.type", string(req.Type)),
		attribute.String("escalation.priority", string(req.Priority)),
		attribute.String("medspa.org_id", req.OrgID),
	)

	escalation := &Escalation{
		ID:                uuid.New(),
		OrgID:             req.OrgID,
		Type:              req.Type,
		Priority:          req.Priority,
		Status:            StatusPending,
		CustomerPhone:     req.CustomerPhone,
		CustomerName:      req.CustomerName,
		LeadID:            req.LeadID,
		ConversationID:    req.ConversationID,
		PaymentID:         req.PaymentID,
		AmountCents:       req.AmountCents,
		Description:       req.Description,
		RecommendedAction: req.RecommendedAction,
		TranscriptSummary: req.TranscriptSummary,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	// Store escalation
	if err := s.storeEscalation(ctx, escalation); err != nil {
		return nil, fmt.Errorf("support: store escalation: %w", err)
	}

	// Send notifications
	if err := s.notifyStaff(ctx, escalation); err != nil {
		s.logger.Error("failed to notify staff", "error", err, "escalation_id", escalation.ID)
		// Don't fail the escalation if notification fails
	}

	s.logger.Info("escalation created",
		"id", escalation.ID,
		"type", escalation.Type,
		"priority", escalation.Priority,
		"customer_phone", escalation.CustomerPhone,
	)

	return escalation, nil
}

// EscalateComplaint creates an escalation from a billing complaint.
func (s *EscalationService) EscalateComplaint(ctx context.Context, orgID string, complaintType, customerPhone, leadID, conversationID, message string, confidence float64) (*Escalation, error) {
	priority := PriorityMedium
	if complaintType == "UNAUTHORIZED" || confidence >= 0.9 {
		priority = PriorityHigh
	}

	recommendedAction := s.getRecommendedAction(EscalationType("COMPLAINT_" + complaintType))

	return s.CreateEscalation(ctx, EscalationRequest{
		OrgID:             orgID,
		Type:              EscalationComplaint,
		Priority:          priority,
		CustomerPhone:     customerPhone,
		LeadID:            leadID,
		ConversationID:    conversationID,
		Description:       fmt.Sprintf("Billing complaint detected: %s (confidence: %.0f%%)\n\nMessage: %s", complaintType, confidence*100, message),
		RecommendedAction: recommendedAction,
	})
}

// EscalateDispute creates an escalation from a payment dispute.
func (s *EscalationService) EscalateDispute(ctx context.Context, orgID, disputeID, paymentID, customerPhone string, amountCents int32, reason string, dueAt time.Time) (*Escalation, error) {
	daysUntilDue := int(time.Until(dueAt).Hours() / 24)
	description := fmt.Sprintf("Payment dispute received\n\nDispute ID: %s\nReason: %s\nAmount: $%.2f\nDue: %s (%d days)\n\nAction required: Submit evidence before due date.", disputeID, reason, float64(amountCents)/100, dueAt.Format("Jan 2, 2006"), daysUntilDue)

	return s.CreateEscalation(ctx, EscalationRequest{
		OrgID:             orgID,
		Type:              EscalationDispute,
		Priority:          PriorityHigh,
		CustomerPhone:     customerPhone,
		PaymentID:         paymentID,
		AmountCents:       amountCents,
		Description:       description,
		RecommendedAction: "1. Review conversation transcript\n2. Gather payment confirmation\n3. Submit evidence via Square Dashboard\n4. Contact customer if appropriate",
	})
}

// EscalateVelocityBlock creates an escalation when velocity limits are exceeded.
func (s *EscalationService) EscalateVelocityBlock(ctx context.Context, orgID, customerPhone, checkType string, currentCount, maxAllowed int) (*Escalation, error) {
	description := fmt.Sprintf("Velocity limit exceeded\n\nPhone: %s\nCheck type: %s\nAttempts: %d (max: %d)\n\nThis may indicate suspicious activity.", customerPhone, checkType, currentCount, maxAllowed)

	return s.CreateEscalation(ctx, EscalationRequest{
		OrgID:             orgID,
		Type:              EscalationVelocityBlock,
		Priority:          PriorityMedium,
		CustomerPhone:     customerPhone,
		Description:       description,
		RecommendedAction: "1. Review customer history\n2. Verify legitimate use case\n3. Whitelist if appropriate\n4. Monitor for fraud patterns",
	})
}

// Acknowledge marks an escalation as acknowledged.
func (s *EscalationService) Acknowledge(ctx context.Context, escalationID uuid.UUID, staffMember string) error {
	now := time.Now()
	query := `
		UPDATE escalations
		SET status = $1, acknowledged_at = $2, acknowledged_by = $3, updated_at = $4
		WHERE id = $5 AND status = $6
	`
	result, err := s.db.ExecContext(ctx, query, StatusAcknowledged, now, staffMember, now, escalationID, StatusPending)
	if err != nil {
		return fmt.Errorf("support: acknowledge escalation: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("support: escalation not found or already acknowledged")
	}

	s.logger.Info("escalation acknowledged", "id", escalationID, "by", staffMember)
	return nil
}

// Resolve marks an escalation as resolved.
func (s *EscalationService) Resolve(ctx context.Context, escalationID uuid.UUID, staffMember, resolution string) error {
	now := time.Now()
	query := `
		UPDATE escalations
		SET status = $1, resolved_at = $2, resolved_by = $3, resolution = $4, updated_at = $5
		WHERE id = $6 AND status != $7
	`
	result, err := s.db.ExecContext(ctx, query, StatusResolved, now, staffMember, resolution, now, escalationID, StatusResolved)
	if err != nil {
		return fmt.Errorf("support: resolve escalation: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("support: escalation not found or already resolved")
	}

	s.logger.Info("escalation resolved", "id", escalationID, "by", staffMember)
	return nil
}

// GetPendingEscalations returns unacknowledged escalations for an org.
func (s *EscalationService) GetPendingEscalations(ctx context.Context, orgID string) ([]*Escalation, error) {
	query := `
		SELECT id, org_id, type, priority, status, customer_phone, customer_name,
			   lead_id, conversation_id, payment_id, amount_cents, description,
			   recommended_action, transcript_summary, acknowledged_at, acknowledged_by,
			   resolved_at, resolved_by, resolution, created_at, updated_at
		FROM escalations
		WHERE org_id = $1 AND status = $2
		ORDER BY
			CASE priority WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 ELSE 3 END,
			created_at ASC
	`
	return s.queryEscalations(ctx, query, orgID, StatusPending)
}

// GetOverdueSLAEscalations returns escalations that exceeded SLA.
func (s *EscalationService) GetOverdueSLAEscalations(ctx context.Context) ([]*Escalation, error) {
	cutoff := time.Now().Add(-time.Duration(s.slaHours) * time.Hour)
	query := `
		SELECT id, org_id, type, priority, status, customer_phone, customer_name,
			   lead_id, conversation_id, payment_id, amount_cents, description,
			   recommended_action, transcript_summary, acknowledged_at, acknowledged_by,
			   resolved_at, resolved_by, resolution, created_at, updated_at
		FROM escalations
		WHERE status = $1 AND created_at < $2
		ORDER BY created_at ASC
	`
	return s.queryEscalations(ctx, query, StatusPending, cutoff)
}

func (s *EscalationService) storeEscalation(ctx context.Context, e *Escalation) error {
	query := `
		INSERT INTO escalations (
			id, org_id, type, priority, status, customer_phone, customer_name,
			lead_id, conversation_id, payment_id, amount_cents, description,
			recommended_action, transcript_summary, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`
	_, err := s.db.ExecContext(ctx, query,
		e.ID, e.OrgID, e.Type, e.Priority, e.Status, e.CustomerPhone, e.CustomerName,
		e.LeadID, e.ConversationID, e.PaymentID, e.AmountCents, e.Description,
		e.RecommendedAction, e.TranscriptSummary, e.CreatedAt, e.UpdatedAt,
	)
	return err
}

func (s *EscalationService) notifyStaff(ctx context.Context, e *Escalation) error {
	if s.notifier == nil {
		return nil
	}

	// Get staff contact info for this org
	staffPhone, staffEmail, err := s.getStaffContacts(ctx, e.OrgID)
	if err != nil {
		return err
	}

	// Send SMS for high/medium priority
	if e.Priority == PriorityHigh || e.Priority == PriorityMedium {
		smsMessage := s.formatSMSNotification(e)
		if staffPhone != "" {
			if err := s.notifier.SendSMS(ctx, staffPhone, smsMessage); err != nil {
				s.logger.Error("failed to send escalation SMS", "error", err)
			}
		}
	}

	// Send detailed email
	if staffEmail != "" {
		subject, body := s.formatEmailNotification(e)
		if err := s.notifier.SendEmail(ctx, staffEmail, subject, body); err != nil {
			s.logger.Error("failed to send escalation email", "error", err)
		}
	}

	return nil
}

func (s *EscalationService) getStaffContacts(ctx context.Context, orgID string) (phone, email string, err error) {
	query := `SELECT operator_phone, contact_email FROM organizations WHERE id = $1`
	err = s.db.QueryRowContext(ctx, query, orgID).Scan(&phone, &email)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return phone, email, err
}

func (s *EscalationService) formatSMSNotification(e *Escalation) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] %s\n", e.Priority, e.Type))
	if e.CustomerPhone != "" {
		sb.WriteString(fmt.Sprintf("Customer: %s\n", e.CustomerPhone))
	}
	if e.AmountCents > 0 {
		sb.WriteString(fmt.Sprintf("Amount: $%.2f\n", float64(e.AmountCents)/100))
	}
	sb.WriteString("Please check your email for details.")
	return sb.String()
}

func (s *EscalationService) formatEmailNotification(e *Escalation) (subject, body string) {
	subject = fmt.Sprintf("[%s Priority] %s Escalation", e.Priority, e.Type)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Escalation ID: %s\n\n", e.ID))
	sb.WriteString(fmt.Sprintf("Type: %s\n", e.Type))
	sb.WriteString(fmt.Sprintf("Priority: %s\n", e.Priority))
	sb.WriteString(fmt.Sprintf("Created: %s\n\n", e.CreatedAt.Format(time.RFC1123)))

	if e.CustomerPhone != "" {
		sb.WriteString(fmt.Sprintf("Customer Phone: %s\n", e.CustomerPhone))
	}
	if e.CustomerName != "" {
		sb.WriteString(fmt.Sprintf("Customer Name: %s\n", e.CustomerName))
	}
	if e.AmountCents > 0 {
		sb.WriteString(fmt.Sprintf("Amount: $%.2f\n", float64(e.AmountCents)/100))
	}

	sb.WriteString("\n--- Description ---\n")
	sb.WriteString(e.Description)
	sb.WriteString("\n\n")

	if e.RecommendedAction != "" {
		sb.WriteString("--- Recommended Action ---\n")
		sb.WriteString(e.RecommendedAction)
		sb.WriteString("\n\n")
	}

	if e.TranscriptSummary != "" {
		sb.WriteString("--- Conversation Summary ---\n")
		sb.WriteString(e.TranscriptSummary)
		sb.WriteString("\n")
	}

	return subject, sb.String()
}

func (s *EscalationService) getRecommendedAction(escalationType EscalationType) string {
	switch escalationType {
	case "COMPLAINT_OVERCHARGE":
		return "1. Review the payment amount\n2. Check service pricing\n3. Contact customer to clarify\n4. Issue refund if error confirmed"
	case "COMPLAINT_UNAUTHORIZED":
		return "1. Review payment authorization\n2. Check conversation for consent\n3. Contact customer immediately\n4. Issue refund if no authorization found"
	case "COMPLAINT_REFUND_REQUEST":
		return "1. Review refund policy eligibility\n2. Check time since payment\n3. Process refund if eligible\n4. Explain policy if not eligible"
	case "COMPLAINT_DOUBLE_CHARGE":
		return "1. Check for duplicate payments\n2. Verify with Square dashboard\n3. Issue refund for duplicate\n4. Apologize to customer"
	default:
		return "1. Review the situation\n2. Contact customer if needed\n3. Take appropriate action\n4. Document resolution"
	}
}

func (s *EscalationService) queryEscalations(ctx context.Context, query string, args ...any) ([]*Escalation, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var escalations []*Escalation
	for rows.Next() {
		var e Escalation
		var leadID, convID, paymentID, ackBy, resBy, resolution sql.NullString
		var ackAt, resAt sql.NullTime

		err := rows.Scan(
			&e.ID, &e.OrgID, &e.Type, &e.Priority, &e.Status, &e.CustomerPhone, &e.CustomerName,
			&leadID, &convID, &paymentID, &e.AmountCents, &e.Description,
			&e.RecommendedAction, &e.TranscriptSummary, &ackAt, &ackBy,
			&resAt, &resBy, &resolution, &e.CreatedAt, &e.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		e.LeadID = leadID.String
		e.ConversationID = convID.String
		e.PaymentID = paymentID.String
		e.AcknowledgedBy = ackBy.String
		e.ResolvedBy = resBy.String
		e.Resolution = resolution.String
		if ackAt.Valid {
			e.AcknowledgedAt = &ackAt.Time
		}
		if resAt.Valid {
			e.ResolvedAt = &resAt.Time
		}

		escalations = append(escalations, &e)
	}

	return escalations, rows.Err()
}
