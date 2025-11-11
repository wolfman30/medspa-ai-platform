package leads

import (
	"context"
	"fmt"
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

