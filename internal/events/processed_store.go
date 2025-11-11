package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProcessedStore records webhook events that were already handled.
type rowQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type ProcessedStore struct {
	pool rowQuerier
}

func NewProcessedStore(pool *pgxpool.Pool) *ProcessedStore {
	if pool == nil {
		panic("events: pgx pool required")
	}
	return &ProcessedStore{pool: pool}
}

func newProcessedStoreWithExec(exec rowQuerier) *ProcessedStore {
	if exec == nil {
		panic("events: exec required")
	}
	return &ProcessedStore{pool: exec}
}

// AlreadyProcessed checks if we've seen this provider event id.
func (s *ProcessedStore) AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	query := `SELECT 1 FROM processed_events WHERE provider = $1 AND event_id = $2`
	var exists int
	if err := s.pool.QueryRow(ctx, query, provider, eventID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("events: check processed: %w", err)
	}
	return true, nil
}

// MarkProcessed inserts an event id for the provider, returning false if it already exists.
func (s *ProcessedStore) MarkProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	query := `
		INSERT INTO processed_events (provider, event_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`
	ct, err := s.pool.Exec(ctx, query, provider, eventID)
	if err != nil {
		return false, fmt.Errorf("events: mark processed: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}
