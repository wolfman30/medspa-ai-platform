package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// CanonicalEvent represents a versioned domain event.
type CanonicalEvent interface {
	EventType() string
}

// Envelope captures transport metadata for canonical events.
type Envelope struct {
	EventID         uuid.UUID       `json:"event_id"`
	EventType       string          `json:"event_type"`
	Aggregate       string          `json:"aggregate"`
	TimestampMicros int64           `json:"timestamp"`
	CorrelationID   string          `json:"correlation_id,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

// EnvelopeOption customizes the generated envelope (useful in tests).
type EnvelopeOption func(*Envelope)

// WithEventID overrides the automatically generated event id.
func WithEventID(id uuid.UUID) EnvelopeOption {
	return func(e *Envelope) {
		if id != uuid.Nil {
			e.EventID = id
		}
	}
}

// WithTimestamp overrides the timestamp stored in microseconds.
func WithTimestamp(ts time.Time) EnvelopeOption {
	return func(e *Envelope) {
		if ts.IsZero() {
			return
		}
		e.TimestampMicros = ts.UTC().UnixMicro()
	}
}

var (
	errMissingAggregate = errors.New("events: aggregate is required")
	errNilEvent         = errors.New("events: canonical event required")
	nowFunc             = time.Now
)

func newEnvelope(aggregate, correlationID string, evt CanonicalEvent, opts ...EnvelopeOption) (Envelope, error) {
	if strings.TrimSpace(aggregate) == "" {
		return Envelope{}, errMissingAggregate
	}
	if evt == nil {
		return Envelope{}, errNilEvent
	}
	eventType := strings.TrimSpace(evt.EventType())
	if eventType == "" {
		return Envelope{}, fmt.Errorf("events: event type missing")
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return Envelope{}, fmt.Errorf("events: marshal canonical payload: %w", err)
	}
	env := Envelope{
		EventID:         uuid.New(),
		EventType:       eventType,
		Aggregate:       strings.TrimSpace(aggregate),
		TimestampMicros: nowFunc().UTC().UnixMicro(),
		CorrelationID:   strings.TrimSpace(correlationID),
		Payload:         append([]byte(nil), payload...),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&env)
		}
	}
	return env, nil
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// AppendCanonicalEvent marshals the envelope, writes it to the outbox inside the provided executor, and returns the envelope.
func AppendCanonicalEvent(ctx context.Context, exec execer, aggregate, correlationID string, evt CanonicalEvent, opts ...EnvelopeOption) (Envelope, error) {
	if exec == nil {
		return Envelope{}, fmt.Errorf("events: exec required")
	}
	env, err := newEnvelope(aggregate, correlationID, evt, opts...)
	if err != nil {
		return Envelope{}, err
	}
	data, err := json.Marshal(env)
	if err != nil {
		return Envelope{}, fmt.Errorf("events: marshal envelope: %w", err)
	}
	query := `
		INSERT INTO outbox (id, aggregate, event_type, payload)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := exec.Exec(ctx, query, env.EventID, env.Aggregate, env.EventType, data); err != nil {
		return Envelope{}, fmt.Errorf("events: append canonical event: %w", err)
	}
	return env, nil
}
