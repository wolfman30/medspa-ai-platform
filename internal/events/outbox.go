package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// OutboxEntry represents a pending event.
type OutboxEntry struct {
	ID        uuid.UUID
	OrgID     string
	Type      string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// DeliveryHandler emits events to downstream transports.
type DeliveryHandler interface {
	Handle(ctx context.Context, entry OutboxEntry) error
}

// OutboxStore persists events for reliable delivery.
type OutboxStore struct {
	pool *pgxpool.Pool
}

func NewOutboxStore(pool *pgxpool.Pool) *OutboxStore {
	if pool == nil {
		panic("events: pgx pool required")
	}
	return &OutboxStore{pool: pool}
}

func (s *OutboxStore) Insert(ctx context.Context, orgID string, eventType string, payload any) (uuid.UUID, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("events: marshal payload: %w", err)
	}
	id := uuid.New()
	query := `
		INSERT INTO outbox (id, org_id, type, payload)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := s.pool.Exec(ctx, query, id, orgID, eventType, data); err != nil {
		return uuid.Nil, fmt.Errorf("events: insert outbox: %w", err)
	}
	return id, nil
}

func (s *OutboxStore) FetchPending(ctx context.Context, limit int32) ([]OutboxEntry, error) {
	query := `
		SELECT id, org_id, type, payload, created_at
		FROM outbox
		WHERE delivered_at IS NULL
		ORDER BY created_at
		LIMIT $1
	`
	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("events: fetch pending: %w", err)
	}
	defer rows.Close()

	var entries []OutboxEntry
	for rows.Next() {
		var entry OutboxEntry
		var payload []byte
		if err := rows.Scan(&entry.ID, &entry.OrgID, &entry.Type, &payload, &entry.CreatedAt); err != nil {
			return nil, fmt.Errorf("events: scan outbox: %w", err)
		}
		entry.Payload = append([]byte(nil), payload...)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *OutboxStore) MarkDelivered(ctx context.Context, id uuid.UUID) (bool, error) {
	query := `
		UPDATE outbox
		SET delivered_at = now()
		WHERE id = $1 AND delivered_at IS NULL
	`
	ct, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return false, fmt.Errorf("events: mark delivered: %w", err)
	}
	return ct.RowsAffected() == 1, nil
}

// Deliverer polls the outbox and invokes the handler.
type Deliverer struct {
	store     *OutboxStore
	handler   DeliveryHandler
	logger    *logging.Logger
	batchSize int32
	interval  time.Duration
}

func NewDeliverer(store *OutboxStore, handler DeliveryHandler, logger *logging.Logger) *Deliverer {
	if logger == nil {
		logger = logging.Default()
	}
	return &Deliverer{
		store:     store,
		handler:   handler,
		logger:    logger,
		batchSize: 25,
		interval:  2 * time.Second,
	}
}

func (d *Deliverer) WithBatchSize(size int32) *Deliverer {
	if size > 0 {
		d.batchSize = size
	}
	return d
}

func (d *Deliverer) WithInterval(interval time.Duration) *Deliverer {
	if interval > 0 {
		d.interval = interval
	}
	return d
}

func (d *Deliverer) Start(ctx context.Context) {
	if d.store == nil || d.handler == nil {
		return
	}
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.drain(ctx)
		}
	}
}

func (d *Deliverer) drain(ctx context.Context) {
	entries, err := d.store.FetchPending(ctx, d.batchSize)
	if err != nil {
		d.logger.Error("outbox fetch failed", "error", err)
		return
	}
	for _, entry := range entries {
		if err := d.handler.Handle(ctx, entry); err != nil {
			d.logger.Error("outbox delivery failed", "error", err, "event_id", entry.ID, "type", entry.Type)
			continue
		}
		if ok, err := d.store.MarkDelivered(ctx, entry.ID); err != nil {
			d.logger.Error("failed to mark outbox delivered", "error", err, "event_id", entry.ID)
		} else if ok {
			d.logger.Debug("outbox delivered", "event_id", entry.ID, "type", entry.Type)
		}
	}
}
