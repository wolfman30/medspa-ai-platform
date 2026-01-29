package messaging

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store persists messaging ACL state in Postgres.
type PgxPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct {
	pool PgxPool
}

func NewStore(pool PgxPool) *Store {
	if pool == nil {
		return nil
	}
	return &Store{pool: pool}
}

func (s *Store) Begin(ctx context.Context) (pgx.Tx, error) {
	return s.pool.Begin(ctx)
}

type HostedOrderRecord struct {
	ID              uuid.UUID
	ClinicID        uuid.UUID
	E164Number      string
	Status          string
	LastError       string
	ProviderOrderID string
}

func (s *Store) UpsertHostedOrder(ctx context.Context, q Querier, record HostedOrderRecord) error {
	if q == nil {
		q = s.pool
	}
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	// Use clinic_id + e164_number as conflict target since provider_order_id may be empty
	// for direct activations (e.g., dev/testing scenarios)
	query := `
		INSERT INTO hosted_number_orders (id, clinic_id, e164_number, status, last_error, provider_order_id)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
		ON CONFLICT (clinic_id, e164_number)
		DO UPDATE SET status = EXCLUDED.status,
			last_error = EXCLUDED.last_error,
			provider_order_id = COALESCE(EXCLUDED.provider_order_id, hosted_number_orders.provider_order_id),
			updated_at = now()
	`
	_, err := q.Exec(ctx, query, record.ID, record.ClinicID, record.E164Number, record.Status, record.LastError, record.ProviderOrderID)
	if err != nil {
		return fmt.Errorf("messaging: upsert hosted order: %w", err)
	}
	return nil
}

func (s *Store) LookupClinicByNumber(ctx context.Context, number string) (uuid.UUID, error) {
	var clinicID uuid.UUID
	query := `
		SELECT clinic_id
		FROM hosted_number_orders
		WHERE e164_number = $1 AND status = 'activated'
		LIMIT 1
	`
	if err := s.pool.QueryRow(ctx, query, number).Scan(&clinicID); err != nil {
		return uuid.Nil, fmt.Errorf("messaging: lookup clinic by number: %w", err)
	}
	return clinicID, nil
}

type BrandRecord struct {
	ID           uuid.UUID
	ClinicID     uuid.UUID
	LegalName    string
	BrandID      string
	Status       string
	Contact      string
	ContactEmail string
	ContactPhone string
}

func (s *Store) InsertBrand(ctx context.Context, q Querier, rec BrandRecord) error {
	if q == nil {
		q = s.pool
	}
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	query := `
		INSERT INTO ten_dlc_brands (
			id, clinic_id, legal_name, brand_id, status,
			contact_name, contact_email, contact_phone
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (brand_id) DO UPDATE
		SET status = EXCLUDED.status,
			contact_name = EXCLUDED.contact_name,
			contact_email = EXCLUDED.contact_email,
			contact_phone = EXCLUDED.contact_phone,
			updated_at = now()
	`
	_, err := q.Exec(ctx, query, rec.ID, rec.ClinicID, rec.LegalName, rec.BrandID, rec.Status, rec.Contact, rec.ContactEmail, rec.ContactPhone)
	if err != nil {
		return fmt.Errorf("messaging: insert brand: %w", err)
	}
	return nil
}

type CampaignRecord struct {
	ID         uuid.UUID
	BrandID    uuid.UUID
	CampaignID string
	Status     string
	UseCase    string
	Samples    []string
	HelpText   string
	StopText   string
}

func (s *Store) InsertCampaign(ctx context.Context, q Querier, rec CampaignRecord) error {
	if q == nil {
		q = s.pool
	}
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	if rec.Samples == nil {
		rec.Samples = []string{}
	}
	sampleJSON, err := json.Marshal(rec.Samples)
	if err != nil {
		return fmt.Errorf("messaging: marshal sample messages: %w", err)
	}
	query := `
		INSERT INTO ten_dlc_campaigns (
			id, brand_id, use_case, sample_messages,
			help_message, stop_message, campaign_id, status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (campaign_id) DO UPDATE
		SET status = EXCLUDED.status,
			help_message = EXCLUDED.help_message,
			stop_message = EXCLUDED.stop_message,
			updated_at = now()
	`
	_, err = q.Exec(ctx, query, rec.ID, rec.BrandID, rec.UseCase, sampleJSON, rec.HelpText, rec.StopText, rec.CampaignID, rec.Status)
	if err != nil {
		return fmt.Errorf("messaging: insert campaign: %w", err)
	}
	return nil
}

type MessageRecord struct {
	ID                uuid.UUID
	ClinicID          uuid.UUID
	From              string
	To                string
	Direction         string
	Body              string
	Media             []string
	ProviderStatus    string
	ProviderMessageID string
	SendAttempts      int
	LastAttemptAt     *time.Time
	NextRetryAt       *time.Time
	DeliveredAt       *time.Time
	FailedAt          *time.Time
}

func (s *Store) InsertMessage(ctx context.Context, q Querier, rec MessageRecord) (uuid.UUID, error) {
	if q == nil {
		q = s.pool
	}
	if rec.Media == nil {
		rec.Media = []string{}
	}
	media, err := json.Marshal(rec.Media)
	if err != nil {
		return uuid.Nil, fmt.Errorf("messaging: marshal media: %w", err)
	}
	query := `
		INSERT INTO messages (
			clinic_id, from_e164, to_e164, direction, body,
			mms_media, provider_status, provider_message_id, delivered_at, failed_at,
			send_attempts, last_attempt_at, next_retry_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id
	`
	var id uuid.UUID
	if err := q.QueryRow(ctx, query, rec.ClinicID, rec.From, rec.To, rec.Direction, rec.Body, media, rec.ProviderStatus, rec.ProviderMessageID, rec.DeliveredAt, rec.FailedAt, rec.SendAttempts, rec.LastAttemptAt, rec.NextRetryAt).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("messaging: insert message: %w", err)
	}
	return id, nil
}

func (s *Store) HasInboundMessage(ctx context.Context, clinicID uuid.UUID, from string, to string) (bool, error) {
	query := `
		SELECT 1 FROM messages
		WHERE clinic_id = $1
			AND direction = 'inbound'
			AND from_e164 = $2
			AND to_e164 = $3
		LIMIT 1
	`
	var exists int
	if err := s.pool.QueryRow(ctx, query, clinicID, from, to).Scan(&exists); err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("messaging: check inbound message: %w", err)
	}
	return true, nil
}

// HasProviderMessage checks whether a message with the provider message id exists.
func (s *Store) HasProviderMessage(ctx context.Context, providerMessageID string) (bool, error) {
	providerMessageID = strings.TrimSpace(providerMessageID)
	if providerMessageID == "" {
		return false, nil
	}
	query := `
		SELECT 1 FROM messages
		WHERE provider_message_id = $1
		LIMIT 1
	`
	var exists int
	if err := s.pool.QueryRow(ctx, query, providerMessageID).Scan(&exists); err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("messaging: check provider message: %w", err)
	}
	return true, nil
}

func (s *Store) UpdateMessageStatus(ctx context.Context, providerMessageID, status string, deliveredAt, failedAt *time.Time) error {
	query := `
		UPDATE messages
		SET provider_status = $2,
			delivered_at = COALESCE($3, delivered_at),
			failed_at = COALESCE($4, failed_at),
			next_retry_at = NULL
		WHERE provider_message_id = $1
	`
	_, err := s.pool.Exec(ctx, query, providerMessageID, status, deliveredAt, failedAt)
	if err != nil {
		return fmt.Errorf("messaging: update message status: %w", err)
	}
	return nil
}

// UpdateMessageStatusByID updates a message's status by its UUID (for outbound messages without provider IDs).
func (s *Store) UpdateMessageStatusByID(ctx context.Context, msgID uuid.UUID, status string, deliveredAt, failedAt *time.Time) error {
	query := `
		UPDATE messages
		SET provider_status = $2,
			delivered_at = COALESCE($3, delivered_at),
			failed_at = COALESCE($4, failed_at),
			next_retry_at = NULL
		WHERE id = $1
	`
	_, err := s.pool.Exec(ctx, query, msgID, status, deliveredAt, failedAt)
	if err != nil {
		return fmt.Errorf("messaging: update message status by id: %w", err)
	}
	return nil
}

// UpdateMessageProviderID stores the provider message ID for an outbound message.
func (s *Store) UpdateMessageProviderID(ctx context.Context, msgID uuid.UUID, providerMessageID string) error {
	providerMessageID = strings.TrimSpace(providerMessageID)
	if providerMessageID == "" {
		return nil
	}
	query := `
		UPDATE messages
		SET provider_message_id = $2
		WHERE id = $1
	`
	_, err := s.pool.Exec(ctx, query, msgID, providerMessageID)
	if err != nil {
		return fmt.Errorf("messaging: update provider message id: %w", err)
	}
	return nil
}

func (s *Store) InsertUnsubscribe(ctx context.Context, q Querier, clinicID uuid.UUID, recipient string, source string) error {
	if q == nil {
		q = s.pool
	}
	query := `
		INSERT INTO unsubscribes (clinic_id, recipient_e164, source)
		VALUES ($1, $2, $3)
		ON CONFLICT (clinic_id, recipient_e164) DO UPDATE
		SET source = EXCLUDED.source,
			updated_at = now()
	`
	if _, err := q.Exec(ctx, query, clinicID, recipient, source); err != nil {
		return fmt.Errorf("messaging: insert unsubscribe: %w", err)
	}
	return nil
}

func (s *Store) DeleteUnsubscribe(ctx context.Context, q Querier, clinicID uuid.UUID, recipient string) error {
	if q == nil {
		q = s.pool
	}
	query := `
		DELETE FROM unsubscribes
		WHERE clinic_id = $1 AND recipient_e164 = $2
	`
	if _, err := q.Exec(ctx, query, clinicID, recipient); err != nil {
		return fmt.Errorf("messaging: delete unsubscribe: %w", err)
	}
	return nil
}

func (s *Store) IsUnsubscribed(ctx context.Context, clinicID uuid.UUID, recipient string) (bool, error) {
	query := `
		SELECT 1 FROM unsubscribes
		WHERE clinic_id = $1 AND recipient_e164 = $2
	`
	var exists int
	if err := s.pool.QueryRow(ctx, query, clinicID, recipient).Scan(&exists); err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("messaging: check unsubscribe: %w", err)
	}
	return true, nil
}

func (s *Store) ScheduleRetry(ctx context.Context, q Querier, id uuid.UUID, status string, nextRetry time.Time) error {
	if q == nil {
		q = s.pool
	}
	query := `
		UPDATE messages
		SET send_attempts = send_attempts + 1,
			provider_status = $2,
			last_attempt_at = now(),
			next_retry_at = $3
		WHERE id = $1
	`
	if _, err := q.Exec(ctx, query, id, status, nextRetry); err != nil {
		return fmt.Errorf("messaging: schedule retry: %w", err)
	}
	return nil
}

func (s *Store) ListRetryCandidates(ctx context.Context, limit int, maxAttempts int) ([]MessageRecord, error) {
	query := `
		SELECT id, clinic_id, from_e164, to_e164, body, mms_media,
			provider_status, provider_message_id, send_attempts, next_retry_at
		FROM messages
		WHERE direction = 'outbound'
			AND send_attempts < $1
			AND (next_retry_at IS NULL OR next_retry_at <= now())
			AND provider_status IN ('failed', 'retry_pending')
		ORDER BY next_retry_at NULLS FIRST, created_at
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, query, maxAttempts, limit)
	if err != nil {
		return nil, fmt.Errorf("messaging: list retry candidates: %w", err)
	}
	defer rows.Close()
	var results []MessageRecord
	for rows.Next() {
		var rec MessageRecord
		var media []byte
		var nextRetry sql.NullTime
		if err := rows.Scan(&rec.ID, &rec.ClinicID, &rec.From, &rec.To, &rec.Body, &media, &rec.ProviderStatus, &rec.ProviderMessageID, &rec.SendAttempts, &nextRetry); err != nil {
			return nil, fmt.Errorf("messaging: scan retry candidate: %w", err)
		}
		if err := json.Unmarshal(media, &rec.Media); err != nil {
			return nil, fmt.Errorf("messaging: decode media: %w", err)
		}
		if nextRetry.Valid {
			value := nextRetry.Time
			rec.NextRetryAt = &value
		}
		results = append(results, rec)
	}
	return results, rows.Err()
}

func (s *Store) PendingHostedOrders(ctx context.Context, limit int) ([]HostedOrderRecord, error) {
	query := `
		SELECT id, clinic_id, e164_number, status, last_error, provider_order_id
		FROM hosted_number_orders
		WHERE status IN ('pending', 'verifying', 'documents_submitted')
		ORDER BY updated_at ASC
		LIMIT $1
	`
	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("messaging: pending hosted orders: %w", err)
	}
	defer rows.Close()
	var out []HostedOrderRecord
	for rows.Next() {
		var rec HostedOrderRecord
		if err := rows.Scan(&rec.ID, &rec.ClinicID, &rec.E164Number, &rec.Status, &rec.LastError, &rec.ProviderOrderID); err != nil {
			return nil, fmt.Errorf("messaging: scan hosted order: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
