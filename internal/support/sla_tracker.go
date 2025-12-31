package support

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var slaTracer = otel.Tracer("medspa/sla-tracker")

// CallbackPromiseType represents types of callback promises.
type CallbackPromiseType string

const (
	PromiseCallback     CallbackPromiseType = "CALLBACK"
	PromiseFollowUp     CallbackPromiseType = "FOLLOW_UP"
	PromiseInformation  CallbackPromiseType = "INFORMATION"
	PromiseAppointment  CallbackPromiseType = "APPOINTMENT"
	PromiseRefundStatus CallbackPromiseType = "REFUND_STATUS"
)

// CallbackPromiseStatus represents the status of a promise.
type CallbackPromiseStatus string

const (
	PromiseStatusPending   CallbackPromiseStatus = "PENDING"
	PromiseStatusReminded  CallbackPromiseStatus = "REMINDED"
	PromiseStatusFulfilled CallbackPromiseStatus = "FULFILLED"
	PromiseStatusExpired   CallbackPromiseStatus = "EXPIRED"
	PromiseStatusCancelled CallbackPromiseStatus = "CANCELLED"
)

// CallbackPromise represents a promise made to a customer.
type CallbackPromise struct {
	ID               uuid.UUID
	OrgID            string
	LeadID           string
	ConversationID   string
	CustomerPhone    string
	CustomerName     string
	Type             CallbackPromiseType
	Status           CallbackPromiseStatus
	PromiseText      string // What was promised
	DueAt            time.Time
	RemindAt         time.Time // When to remind staff
	ReminderSent     bool
	FulfilledAt      *time.Time
	FulfilledBy      string
	FulfillmentNotes string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// SLAConfig contains SLA timing configuration.
type SLAConfig struct {
	DefaultCallbackHours int // Default hours until callback is due
	ReminderBeforeHours  int // Hours before due to send reminder
	EscalationAfterHours int // Hours after due to escalate
}

// DefaultSLAConfig returns default SLA configuration.
func DefaultSLAConfig() SLAConfig {
	return SLAConfig{
		DefaultCallbackHours: 4, // 4 hours to call back by default
		ReminderBeforeHours:  1, // Remind 1 hour before due
		EscalationAfterHours: 2, // Escalate 2 hours after overdue
	}
}

// SLATracker tracks callback promises and SLA compliance.
type SLATracker struct {
	db                *sql.DB
	logger            *logging.Logger
	notifier          NotificationChannel
	escalationService *EscalationService
	config            SLAConfig
}

// NewSLATracker creates a new SLA tracker.
func NewSLATracker(db *sql.DB, notifier NotificationChannel, escalation *EscalationService, logger *logging.Logger) *SLATracker {
	if logger == nil {
		logger = logging.Default()
	}
	return &SLATracker{
		db:                db,
		logger:            logger,
		notifier:          notifier,
		escalationService: escalation,
		config:            DefaultSLAConfig(),
	}
}

// CreatePromiseRequest contains details for creating a callback promise.
type CreatePromiseRequest struct {
	OrgID          string
	LeadID         string
	ConversationID string
	CustomerPhone  string
	CustomerName   string
	Type           CallbackPromiseType
	PromiseText    string
	DueAt          *time.Time // Optional, uses default if nil
}

// CreatePromise records a new callback promise.
func (s *SLATracker) CreatePromise(ctx context.Context, req CreatePromiseRequest) (*CallbackPromise, error) {
	ctx, span := slaTracer.Start(ctx, "sla.create_promise")
	defer span.End()
	span.SetAttributes(
		attribute.String("medspa.org_id", req.OrgID),
		attribute.String("promise.type", string(req.Type)),
	)

	now := time.Now()

	// Calculate due time
	dueAt := now.Add(time.Duration(s.config.DefaultCallbackHours) * time.Hour)
	if req.DueAt != nil {
		dueAt = *req.DueAt
	}

	// Calculate reminder time
	remindAt := dueAt.Add(-time.Duration(s.config.ReminderBeforeHours) * time.Hour)
	if remindAt.Before(now) {
		remindAt = now.Add(30 * time.Minute) // At least 30 minutes from now
	}

	promise := &CallbackPromise{
		ID:             uuid.New(),
		OrgID:          req.OrgID,
		LeadID:         req.LeadID,
		ConversationID: req.ConversationID,
		CustomerPhone:  req.CustomerPhone,
		CustomerName:   req.CustomerName,
		Type:           req.Type,
		Status:         PromiseStatusPending,
		PromiseText:    req.PromiseText,
		DueAt:          dueAt,
		RemindAt:       remindAt,
		ReminderSent:   false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.storePromise(ctx, promise); err != nil {
		return nil, fmt.Errorf("support: store promise: %w", err)
	}

	s.logger.Info("callback promise created",
		"id", promise.ID,
		"type", promise.Type,
		"due_at", promise.DueAt,
		"customer_phone", promise.CustomerPhone,
	)

	return promise, nil
}

// FulfillPromise marks a promise as fulfilled.
func (s *SLATracker) FulfillPromise(ctx context.Context, promiseID uuid.UUID, staffMember, notes string) error {
	now := time.Now()
	query := `
		UPDATE callback_promises
		SET status = $1, fulfilled_at = $2, fulfilled_by = $3, fulfillment_notes = $4, updated_at = $5
		WHERE id = $6 AND status IN ($7, $8)
	`
	result, err := s.db.ExecContext(ctx, query,
		PromiseStatusFulfilled, now, staffMember, notes, now,
		promiseID, PromiseStatusPending, PromiseStatusReminded,
	)
	if err != nil {
		return fmt.Errorf("support: fulfill promise: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("support: promise not found or already fulfilled")
	}

	s.logger.Info("callback promise fulfilled", "id", promiseID, "by", staffMember)
	return nil
}

// CancelPromise marks a promise as cancelled.
func (s *SLATracker) CancelPromise(ctx context.Context, promiseID uuid.UUID, reason string) error {
	now := time.Now()
	query := `
		UPDATE callback_promises
		SET status = $1, fulfillment_notes = $2, updated_at = $3
		WHERE id = $4 AND status = $5
	`
	result, err := s.db.ExecContext(ctx, query,
		PromiseStatusCancelled, reason, now, promiseID, PromiseStatusPending,
	)
	if err != nil {
		return fmt.Errorf("support: cancel promise: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("support: promise not found or cannot be cancelled")
	}

	return nil
}

// ProcessReminders checks for promises due for reminder and sends notifications.
func (s *SLATracker) ProcessReminders(ctx context.Context) error {
	ctx, span := slaTracer.Start(ctx, "sla.process_reminders")
	defer span.End()

	// Get promises due for reminder
	promises, err := s.getPromisesDueForReminder(ctx)
	if err != nil {
		return fmt.Errorf("support: get reminder promises: %w", err)
	}

	for _, promise := range promises {
		if err := s.sendReminder(ctx, promise); err != nil {
			s.logger.Error("failed to send reminder", "error", err, "promise_id", promise.ID)
			continue
		}

		// Mark reminder as sent
		if err := s.markReminderSent(ctx, promise.ID); err != nil {
			s.logger.Error("failed to mark reminder sent", "error", err, "promise_id", promise.ID)
		}
	}

	span.SetAttributes(attribute.Int("reminders_sent", len(promises)))
	return nil
}

// ProcessExpiredPromises checks for overdue promises and escalates.
func (s *SLATracker) ProcessExpiredPromises(ctx context.Context) error {
	ctx, span := slaTracer.Start(ctx, "sla.process_expired")
	defer span.End()

	// Get overdue promises
	cutoff := time.Now().Add(-time.Duration(s.config.EscalationAfterHours) * time.Hour)
	promises, err := s.getOverduePromises(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("support: get overdue promises: %w", err)
	}

	for _, promise := range promises {
		// Mark as expired
		if err := s.markExpired(ctx, promise.ID); err != nil {
			s.logger.Error("failed to mark expired", "error", err, "promise_id", promise.ID)
			continue
		}

		// Create escalation
		if s.escalationService != nil {
			_, err := s.escalationService.CreateEscalation(ctx, EscalationRequest{
				OrgID:             promise.OrgID,
				Type:              EscalationCallbackOverdue,
				Priority:          PriorityMedium,
				CustomerPhone:     promise.CustomerPhone,
				CustomerName:      promise.CustomerName,
				LeadID:            promise.LeadID,
				ConversationID:    promise.ConversationID,
				Description:       fmt.Sprintf("Callback promise expired\n\nPromise: %s\nDue: %s\nOverdue by: %s", promise.PromiseText, promise.DueAt.Format(time.RFC1123), time.Since(promise.DueAt).Round(time.Minute)),
				RecommendedAction: "1. Contact customer immediately\n2. Apologize for delay\n3. Fulfill the promise\n4. Document resolution",
			})
			if err != nil {
				s.logger.Error("failed to create escalation for expired promise", "error", err, "promise_id", promise.ID)
			}
		}
	}

	span.SetAttributes(attribute.Int("expired_count", len(promises)))
	return nil
}

// GetPendingPromises returns pending promises for an organization.
func (s *SLATracker) GetPendingPromises(ctx context.Context, orgID string) ([]*CallbackPromise, error) {
	query := `
		SELECT id, org_id, lead_id, conversation_id, customer_phone, customer_name,
			   type, status, promise_text, due_at, remind_at, reminder_sent,
			   fulfilled_at, fulfilled_by, fulfillment_notes, created_at, updated_at
		FROM callback_promises
		WHERE org_id = $1 AND status IN ($2, $3)
		ORDER BY due_at ASC
	`
	return s.queryPromises(ctx, query, orgID, PromiseStatusPending, PromiseStatusReminded)
}

// GetPromisesByLead returns promises for a specific lead.
func (s *SLATracker) GetPromisesByLead(ctx context.Context, leadID string) ([]*CallbackPromise, error) {
	query := `
		SELECT id, org_id, lead_id, conversation_id, customer_phone, customer_name,
			   type, status, promise_text, due_at, remind_at, reminder_sent,
			   fulfilled_at, fulfilled_by, fulfillment_notes, created_at, updated_at
		FROM callback_promises
		WHERE lead_id = $1
		ORDER BY created_at DESC
	`
	return s.queryPromises(ctx, query, leadID)
}

// GetSLAMetrics returns SLA compliance metrics for an organization.
func (s *SLATracker) GetSLAMetrics(ctx context.Context, orgID string, since time.Time) (*SLAMetrics, error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE status = 'FULFILLED') as fulfilled,
			COUNT(*) FILTER (WHERE status = 'EXPIRED') as expired,
			COUNT(*) FILTER (WHERE status IN ('PENDING', 'REMINDED')) as pending,
			COUNT(*) FILTER (WHERE status = 'FULFILLED' AND fulfilled_at <= due_at) as on_time,
			AVG(EXTRACT(EPOCH FROM (fulfilled_at - created_at))/3600) FILTER (WHERE status = 'FULFILLED') as avg_response_hours
		FROM callback_promises
		WHERE org_id = $1 AND created_at >= $2
	`

	var metrics SLAMetrics
	var avgHours sql.NullFloat64
	err := s.db.QueryRowContext(ctx, query, orgID, since).Scan(
		&metrics.Total,
		&metrics.Fulfilled,
		&metrics.Expired,
		&metrics.Pending,
		&metrics.OnTime,
		&avgHours,
	)
	if err != nil {
		return nil, fmt.Errorf("support: sla metrics: %w", err)
	}

	if avgHours.Valid {
		metrics.AvgResponseHours = avgHours.Float64
	}
	if metrics.Fulfilled > 0 {
		metrics.OnTimeRate = float64(metrics.OnTime) / float64(metrics.Fulfilled) * 100
	}
	if metrics.Total > 0 {
		metrics.FulfillmentRate = float64(metrics.Fulfilled) / float64(metrics.Total) * 100
	}

	return &metrics, nil
}

// SLAMetrics contains SLA compliance statistics.
type SLAMetrics struct {
	Total            int
	Fulfilled        int
	Expired          int
	Pending          int
	OnTime           int
	OnTimeRate       float64 // Percentage of fulfilled promises that were on time
	FulfillmentRate  float64 // Percentage of all promises that were fulfilled
	AvgResponseHours float64 // Average time to fulfill in hours
}

func (s *SLATracker) storePromise(ctx context.Context, p *CallbackPromise) error {
	query := `
		INSERT INTO callback_promises (
			id, org_id, lead_id, conversation_id, customer_phone, customer_name,
			type, status, promise_text, due_at, remind_at, reminder_sent,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := s.db.ExecContext(ctx, query,
		p.ID, p.OrgID, p.LeadID, p.ConversationID, p.CustomerPhone, p.CustomerName,
		p.Type, p.Status, p.PromiseText, p.DueAt, p.RemindAt, p.ReminderSent,
		p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (s *SLATracker) getPromisesDueForReminder(ctx context.Context) ([]*CallbackPromise, error) {
	now := time.Now()
	query := `
		SELECT id, org_id, lead_id, conversation_id, customer_phone, customer_name,
			   type, status, promise_text, due_at, remind_at, reminder_sent,
			   fulfilled_at, fulfilled_by, fulfillment_notes, created_at, updated_at
		FROM callback_promises
		WHERE status = $1 AND reminder_sent = false AND remind_at <= $2
		ORDER BY due_at ASC
	`
	return s.queryPromises(ctx, query, PromiseStatusPending, now)
}

func (s *SLATracker) getOverduePromises(ctx context.Context, cutoff time.Time) ([]*CallbackPromise, error) {
	query := `
		SELECT id, org_id, lead_id, conversation_id, customer_phone, customer_name,
			   type, status, promise_text, due_at, remind_at, reminder_sent,
			   fulfilled_at, fulfilled_by, fulfillment_notes, created_at, updated_at
		FROM callback_promises
		WHERE status IN ($1, $2) AND due_at < $3
		ORDER BY due_at ASC
	`
	return s.queryPromises(ctx, query, PromiseStatusPending, PromiseStatusReminded, cutoff)
}

func (s *SLATracker) sendReminder(ctx context.Context, promise *CallbackPromise) error {
	if s.notifier == nil {
		return nil
	}

	// Get staff contact
	var staffPhone string
	err := s.db.QueryRowContext(ctx,
		`SELECT operator_phone FROM organizations WHERE id = $1`,
		promise.OrgID,
	).Scan(&staffPhone)
	if err != nil || staffPhone == "" {
		return nil // No staff phone configured
	}

	timeUntilDue := time.Until(promise.DueAt).Round(time.Minute)
	message := fmt.Sprintf("Callback reminder: %s (%s) - Due in %s\n%s",
		promise.CustomerPhone,
		promise.CustomerName,
		timeUntilDue,
		promise.PromiseText,
	)

	return s.notifier.SendSMS(ctx, staffPhone, message)
}

func (s *SLATracker) markReminderSent(ctx context.Context, promiseID uuid.UUID) error {
	query := `UPDATE callback_promises SET reminder_sent = true, status = $1, updated_at = $2 WHERE id = $3`
	_, err := s.db.ExecContext(ctx, query, PromiseStatusReminded, time.Now(), promiseID)
	return err
}

func (s *SLATracker) markExpired(ctx context.Context, promiseID uuid.UUID) error {
	query := `UPDATE callback_promises SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := s.db.ExecContext(ctx, query, PromiseStatusExpired, time.Now(), promiseID)
	return err
}

func (s *SLATracker) queryPromises(ctx context.Context, query string, args ...any) ([]*CallbackPromise, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var promises []*CallbackPromise
	for rows.Next() {
		var p CallbackPromise
		var leadID, convID, fulfilledBy, notes sql.NullString
		var fulfilledAt sql.NullTime

		err := rows.Scan(
			&p.ID, &p.OrgID, &leadID, &convID, &p.CustomerPhone, &p.CustomerName,
			&p.Type, &p.Status, &p.PromiseText, &p.DueAt, &p.RemindAt, &p.ReminderSent,
			&fulfilledAt, &fulfilledBy, &notes, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		p.LeadID = leadID.String
		p.ConversationID = convID.String
		p.FulfilledBy = fulfilledBy.String
		p.FulfillmentNotes = notes.String
		if fulfilledAt.Valid {
			p.FulfilledAt = &fulfilledAt.Time
		}

		promises = append(promises, &p)
	}

	return promises, rows.Err()
}
