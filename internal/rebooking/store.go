package rebooking

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DB abstracts the pgx query interface for testing.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store provides CRUD operations for rebook_reminders.
type Store struct {
	db DB
}

// NewStore creates a new rebooking store.
func NewStore(db DB) *Store {
	return &Store{db: db}
}

// Create inserts a new rebooking reminder.
func (s *Store) Create(ctx context.Context, r *Reminder) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	now := time.Now().UTC()
	r.CreatedAt = now
	r.UpdatedAt = now
	if r.Status == "" {
		r.Status = StatusPending
	}
	if r.Channel == "" {
		r.Channel = ChannelSMS
	}

	_, err := s.db.Exec(ctx, `
		INSERT INTO rebook_reminders (id, org_id, patient_id, phone, patient_name, service, provider, booked_at, rebook_after, status, channel, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		r.ID, r.OrgID, r.PatientID, r.Phone, r.PatientName, r.Service, r.Provider,
		r.BookedAt, r.RebookAfter, string(r.Status), string(r.Channel), r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("rebooking: create reminder: %w", err)
	}
	return nil
}

// ListDue returns all pending reminders whose rebook_after is on or before the given time.
func (s *Store) ListDue(ctx context.Context, asOf time.Time) ([]Reminder, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, org_id, patient_id, phone, patient_name, service, provider, booked_at, rebook_after, status, channel, sent_at, dismissed_at, rebooked_at, created_at, updated_at
		FROM rebook_reminders
		WHERE status = 'pending' AND rebook_after <= $1
		ORDER BY rebook_after ASC`, asOf)
	if err != nil {
		return nil, fmt.Errorf("rebooking: list due: %w", err)
	}
	defer rows.Close()
	return scanReminders(rows)
}

// ListByOrg returns reminders for a clinic, optionally filtered by status.
func (s *Store) ListByOrg(ctx context.Context, orgID string, status *ReminderStatus, limit int) ([]Reminder, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows pgx.Rows
	var err error
	if status != nil {
		rows, err = s.db.Query(ctx, `
			SELECT id, org_id, patient_id, phone, patient_name, service, provider, booked_at, rebook_after, status, channel, sent_at, dismissed_at, rebooked_at, created_at, updated_at
			FROM rebook_reminders
			WHERE org_id = $1 AND status = $2
			ORDER BY rebook_after ASC LIMIT $3`, orgID, string(*status), limit)
	} else {
		rows, err = s.db.Query(ctx, `
			SELECT id, org_id, patient_id, phone, patient_name, service, provider, booked_at, rebook_after, status, channel, sent_at, dismissed_at, rebooked_at, created_at, updated_at
			FROM rebook_reminders
			WHERE org_id = $1
			ORDER BY rebook_after ASC LIMIT $2`, orgID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("rebooking: list by org: %w", err)
	}
	defer rows.Close()
	return scanReminders(rows)
}

// MarkSent transitions a reminder from pending → sent.
func (s *Store) MarkSent(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	tag, err := s.db.Exec(ctx, `
		UPDATE rebook_reminders SET status = 'sent', sent_at = $1, updated_at = $1
		WHERE id = $2 AND status = 'pending'`, now, id)
	if err != nil {
		return fmt.Errorf("rebooking: mark sent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("rebooking: mark sent: no pending reminder with id %s", id)
	}
	return nil
}

// MarkBooked transitions a reminder → booked (patient rebooked).
func (s *Store) MarkBooked(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(ctx, `
		UPDATE rebook_reminders SET status = 'booked', rebooked_at = $1, updated_at = $1
		WHERE id = $2 AND status IN ('pending', 'sent')`, now, id)
	if err != nil {
		return fmt.Errorf("rebooking: mark booked: %w", err)
	}
	return nil
}

// Dismiss transitions a reminder → dismissed (patient opted out).
func (s *Store) Dismiss(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(ctx, `
		UPDATE rebook_reminders SET status = 'dismissed', dismissed_at = $1, updated_at = $1
		WHERE id = $2 AND status IN ('pending', 'sent')`, now, id)
	if err != nil {
		return fmt.Errorf("rebooking: dismiss: %w", err)
	}
	return nil
}

// DismissByPhone dismisses all sent reminders for a phone+org (when patient says STOP/no thanks).
func (s *Store) DismissByPhone(ctx context.Context, orgID, phone string) (int64, error) {
	now := time.Now().UTC()
	tag, err := s.db.Exec(ctx, `
		UPDATE rebook_reminders SET status = 'dismissed', dismissed_at = $1, updated_at = $1
		WHERE org_id = $2 AND phone = $3 AND status = 'sent'`, now, orgID, phone)
	if err != nil {
		return 0, fmt.Errorf("rebooking: dismiss by phone: %w", err)
	}
	return tag.RowsAffected(), nil
}

// FindSentByPhone returns the most recent sent reminder for a phone+org (for handling replies).
func (s *Store) FindSentByPhone(ctx context.Context, orgID, phone string) (*Reminder, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, org_id, patient_id, phone, patient_name, service, provider, booked_at, rebook_after, status, channel, sent_at, dismissed_at, rebooked_at, created_at, updated_at
		FROM rebook_reminders
		WHERE org_id = $1 AND phone = $2 AND status = 'sent'
		ORDER BY sent_at DESC LIMIT 1`, orgID, phone)
	if err != nil {
		return nil, fmt.Errorf("rebooking: find sent by phone: %w", err)
	}
	defer rows.Close()
	reminders, err := scanReminders(rows)
	if err != nil {
		return nil, err
	}
	if len(reminders) == 0 {
		return nil, nil
	}
	return &reminders[0], nil
}

// Stats returns aggregated rebooking metrics for the admin dashboard.
func (s *Store) Stats(ctx context.Context, orgID string) (*DashboardStats, error) {
	row := s.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'pending') AS upcoming,
			COUNT(*) FILTER (WHERE status = 'sent') AS sent,
			COUNT(*) FILTER (WHERE status = 'booked') AS rebooked,
			COUNT(*) FILTER (WHERE status = 'dismissed') AS dismissed
		FROM rebook_reminders
		WHERE org_id = $1`, orgID)

	var stats DashboardStats
	err := row.Scan(&stats.UpcomingCount, &stats.SentCount, &stats.RebookedCount, &stats.DismissedCount)
	if err != nil {
		return nil, fmt.Errorf("rebooking: stats: %w", err)
	}
	total := stats.SentCount + stats.RebookedCount + stats.DismissedCount
	if total > 0 {
		stats.ConversionPct = float64(stats.RebookedCount) / float64(total) * 100
	}
	return &stats, nil
}

func scanReminders(rows pgx.Rows) ([]Reminder, error) {
	var result []Reminder
	for rows.Next() {
		var r Reminder
		var status, channel string
		err := rows.Scan(
			&r.ID, &r.OrgID, &r.PatientID, &r.Phone, &r.PatientName,
			&r.Service, &r.Provider, &r.BookedAt, &r.RebookAfter,
			&status, &channel,
			&r.SentAt, &r.DismissedAt, &r.RebookedAt,
			&r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("rebooking: scan reminder: %w", err)
		}
		r.Status = ReminderStatus(status)
		r.Channel = Channel(channel)
		result = append(result, r)
	}
	return result, rows.Err()
}
