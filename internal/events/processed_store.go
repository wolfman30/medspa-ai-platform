package events

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
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
	eventUUID, _, _, err := normalizeProcessedEvent(provider, eventID)
	if err != nil {
		return false, err
	}
	query := `SELECT 1 FROM processed_events WHERE event_id = $1`
	var exists int
	if err := s.pool.QueryRow(ctx, query, eventUUID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("events: check processed: %w", err)
	}
	return true, nil
}

// MarkProcessed inserts an event id for the provider, returning false if it already exists.
func (s *ProcessedStore) MarkProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	eventUUID, normalizedProvider, normalizedEventID, err := normalizeProcessedEvent(provider, eventID)
	if err != nil {
		return false, err
	}
	query := `
		INSERT INTO processed_events (event_id, provider, external_event_id)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''))
		ON CONFLICT DO NOTHING
	`
	ct, err := s.pool.Exec(ctx, query, eventUUID, normalizedProvider, normalizedEventID)
	if err != nil {
		return false, fmt.Errorf("events: mark processed: %w", err)
	}
	return ct.RowsAffected() > 0, nil
}

var processedNamespace = uuid.MustParse("1c4b4ef0-0f1f-4f8b-8a9c-7c0fba51cdbd")

func normalizeProcessedEvent(provider, eventID string) (uuid.UUID, string, string, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return uuid.Nil, "", "", fmt.Errorf("events: event id required")
	}
	provider = strings.TrimSpace(provider)
	key := provider + ":" + eventID
	return uuid.NewSHA1(processedNamespace, []byte(key)), provider, eventID, nil
}
