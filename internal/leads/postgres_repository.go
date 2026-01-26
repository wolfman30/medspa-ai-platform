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
		SELECT id, org_id, name, email, phone, message, source, created_at,
		       COALESCE(service_interest, '') as service_interest,
		       COALESCE(patient_type, '') as patient_type,
		       COALESCE(past_services, '') as past_services,
		       COALESCE(preferred_days, '') as preferred_days,
		       COALESCE(preferred_times, '') as preferred_times,
		       COALESCE(scheduling_notes, '') as scheduling_notes,
		       COALESCE(deposit_status, '') as deposit_status,
		       COALESCE(priority_level, '') as priority_level
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
		&lead.ServiceInterest,
		&lead.PatientType,
		&lead.PastServices,
		&lead.PreferredDays,
		&lead.PreferredTimes,
		&lead.SchedulingNotes,
		&lead.DepositStatus,
		&lead.PriorityLevel,
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
		SELECT id, org_id, name, email, phone, message, source, created_at,
		       COALESCE(service_interest, '') as service_interest,
		       COALESCE(patient_type, '') as patient_type,
		       COALESCE(past_services, '') as past_services,
		       COALESCE(preferred_days, '') as preferred_days,
		       COALESCE(preferred_times, '') as preferred_times,
		       COALESCE(scheduling_notes, '') as scheduling_notes,
		       COALESCE(deposit_status, '') as deposit_status,
		       COALESCE(priority_level, '') as priority_level
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
		&lead.ServiceInterest,
		&lead.PatientType,
		&lead.PastServices,
		&lead.PreferredDays,
		&lead.PreferredTimes,
		&lead.SchedulingNotes,
		&lead.DepositStatus,
		&lead.PriorityLevel,
	); err == nil {
		return &lead, nil
	} else if err != pgx.ErrNoRows {
		return nil, fmt.Errorf("leads: lookup by phone failed: %w", err)
	}

	// Use defaultName as-is; if empty, keep it empty - name will be extracted from conversation
	// Notification service handles empty names by showing "A patient"
	name := strings.TrimSpace(defaultName)
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
	// Build dynamic query - only update fields if provided (don't overwrite with empty)
	query := `
		UPDATE leads
		SET service_interest = COALESCE(NULLIF($2, ''), service_interest),
		    patient_type = COALESCE(NULLIF($3, ''), patient_type),
		    past_services = COALESCE(NULLIF($4, ''), past_services),
		    preferred_days = COALESCE(NULLIF($5, ''), preferred_days),
		    preferred_times = COALESCE(NULLIF($6, ''), preferred_times),
		    scheduling_notes = COALESCE(NULLIF($7, ''), scheduling_notes),
		    name = COALESCE(NULLIF($8, ''), name)
		WHERE id = $1
	`
	result, err := r.pool.Exec(ctx, query,
		leadID,
		prefs.ServiceInterest,
		prefs.PatientType,
		prefs.PastServices,
		prefs.PreferredDays,
		prefs.PreferredTimes,
		prefs.Notes,
		prefs.Name,
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

// ListByOrg retrieves leads for an organization with optional filtering
func (r *PostgresRepository) ListByOrg(ctx context.Context, orgID string, filter ListLeadsFilter) ([]*Lead, error) {
	// Build query with optional filter
	query := `
		SELECT id, org_id, name, email, phone, message, source, created_at,
		       COALESCE(service_interest, '') as service_interest,
		       COALESCE(patient_type, '') as patient_type,
		       COALESCE(past_services, '') as past_services,
		       COALESCE(preferred_days, '') as preferred_days,
		       COALESCE(preferred_times, '') as preferred_times,
		       COALESCE(scheduling_notes, '') as scheduling_notes,
		       COALESCE(deposit_status, '') as deposit_status,
		       COALESCE(priority_level, '') as priority_level
		FROM leads
		WHERE org_id = $1
	`
	args := []any{orgID}
	argNum := 2

	if filter.DepositStatus != "" {
		query += fmt.Sprintf(" AND deposit_status = $%d", argNum)
		args = append(args, filter.DepositStatus)
		argNum++
	}

	query += " ORDER BY created_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" LIMIT $%d", argNum)
	args = append(args, limit)
	argNum++

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argNum)
		args = append(args, filter.Offset)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("leads: list query failed: %w", err)
	}
	defer rows.Close()

	var results []*Lead
	for rows.Next() {
		var lead Lead
		if err := rows.Scan(
			&lead.ID,
			&lead.OrgID,
			&lead.Name,
			&lead.Email,
			&lead.Phone,
			&lead.Message,
			&lead.Source,
			&lead.CreatedAt,
			&lead.ServiceInterest,
			&lead.PatientType,
			&lead.PastServices,
			&lead.PreferredDays,
			&lead.PreferredTimes,
			&lead.SchedulingNotes,
			&lead.DepositStatus,
			&lead.PriorityLevel,
		); err != nil {
			return nil, fmt.Errorf("leads: scan failed: %w", err)
		}
		results = append(results, &lead)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("leads: rows error: %w", err)
	}

	return results, nil
}
