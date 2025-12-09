package leads

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository stores leads in the relational database.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository initializes a repo backed by pgxpool.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	if pool == nil {
		panic("leads: pgx pool required")
	}
	return &PostgresRepository{pool: pool}
}

// Create inserts a new row.
func (r *PostgresRepository) Create(ctx context.Context, req *CreateLeadRequest) (*Lead, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	id := uuid.New()
	query := `
		INSERT INTO leads (id, org_id, name, email, phone, message, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`
	var createdAt time.Time
	if err := r.pool.QueryRow(ctx, query,
		id,
		req.OrgID,
		req.Name,
		req.Email,
		req.Phone,
		req.Message,
		req.Source,
	).Scan(&createdAt); err != nil {
		return nil, fmt.Errorf("leads: insert failed: %w", err)
	}

	return &Lead{
		ID:        id.String(),
		OrgID:     req.OrgID,
		Name:      req.Name,
		Email:     req.Email,
		Phone:     req.Phone,
		Message:   req.Message,
		Source:    req.Source,
		CreatedAt: createdAt,
	}, nil
}

// GetByID fetches a lead scoped to the org.
func (r *PostgresRepository) GetByID(ctx context.Context, orgID string, id string) (*Lead, error) {
	query := `
		SELECT id, org_id, name, email, phone, message, source, created_at
		FROM leads
		WHERE id = $1 AND org_id = $2
	`
	row := r.pool.QueryRow(ctx, query, id, orgID)
	var lead Lead
	if err := row.Scan(
		&lead.ID,
		&lead.OrgID,
		&lead.Name,
		&lead.Email,
		&lead.Phone,
		&lead.Message,
		&lead.Source,
		&lead.CreatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrLeadNotFound
		}
		return nil, fmt.Errorf("leads: select failed: %w", err)
	}
	return &lead, nil
}

// GetOrCreateByPhone finds the most recent lead for an org/phone or creates a new one.
func (r *PostgresRepository) GetOrCreateByPhone(ctx context.Context, orgID string, phone string, source string, defaultName string) (*Lead, error) {
	phone = strings.TrimSpace(phone)
	orgID = strings.TrimSpace(orgID)
	if phone == "" || orgID == "" {
		return nil, fmt.Errorf("leads: org and phone are required")
	}
	query := `
		SELECT id, org_id, name, email, phone, message, source, created_at
		FROM leads
		WHERE org_id = $1 AND phone = $2
		ORDER BY created_at DESC
		LIMIT 1
	`
	var lead Lead
	if err := r.pool.QueryRow(ctx, query, orgID, phone).Scan(
		&lead.ID,
		&lead.OrgID,
		&lead.Name,
		&lead.Email,
		&lead.Phone,
		&lead.Message,
		&lead.Source,
		&lead.CreatedAt,
	); err == nil {
		return &lead, nil
	} else if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("leads: lookup by phone failed: %w", err)
	}

	name := strings.TrimSpace(defaultName)
	if name == "" {
		name = phone
	}
	req := &CreateLeadRequest{
		OrgID:  orgID,
		Name:   name,
		Phone:  phone,
		Source: source,
	}
	return r.Create(ctx, req)
}

// UpdateSchedulingPreferences updates a lead's scheduling preferences
func (r *PostgresRepository) UpdateSchedulingPreferences(ctx context.Context, leadID string, prefs SchedulingPreferences) error {
	query := `
		UPDATE leads
		SET service_interest = $2,
		    patient_type = $3,
		    preferred_days = $4,
		    preferred_times = $5,
		    scheduling_notes = $6
		WHERE id = $1
	`
	result, err := r.pool.Exec(ctx, query,
		leadID,
		prefs.ServiceInterest,
		prefs.PatientType,
		prefs.PreferredDays,
		prefs.PreferredTimes,
		prefs.Notes,
	)
	if err != nil {
		return fmt.Errorf("leads: update preferences failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrLeadNotFound
	}
	return nil
}

// UpdateDepositStatus updates a lead's deposit status and priority level
func (r *PostgresRepository) UpdateDepositStatus(ctx context.Context, leadID string, status string, priority string) error {
	query := `
		UPDATE leads
		SET deposit_status = $2,
		    priority_level = $3
		WHERE id = $1
	`
	result, err := r.pool.Exec(ctx, query, leadID, status, priority)
	if err != nil {
		return fmt.Errorf("leads: update deposit status failed: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrLeadNotFound
	}
	return nil
}
