package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const conversationTTL = 24 * time.Hour

type historyStore struct {
	redis  *redis.Client
	tracer trace.Tracer
}

func newHistoryStore(redis *redis.Client, tracer trace.Tracer) *historyStore {
	if redis == nil {
		panic("conversation: redis client cannot be nil")
	}
	if tracer == nil {
		tracer = otel.Tracer("medspa.internal.conversation.history")
	}
	return &historyStore{
		redis:  redis,
		tracer: tracer,
	}
}

func (s *historyStore) Save(ctx context.Context, conversationID string, history []ChatMessage) error {
	ctx, span := s.tracer.Start(ctx, "conversation.save_history")
	defer span.End()

	data, err := json.Marshal(history)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("conversation: failed to marshal history: %w", err)
	}
	if err := s.redis.Set(ctx, conversationKey(conversationID), data, conversationTTL).Err(); err != nil {
		span.RecordError(err)
		return fmt.Errorf("conversation: failed to persist history: %w", err)
	}
	return nil
}

func (s *historyStore) Load(ctx context.Context, conversationID string) ([]ChatMessage, error) {
	ctx, span := s.tracer.Start(ctx, "conversation.load_history")
	defer span.End()

	data, err := s.redis.Get(ctx, conversationKey(conversationID)).Bytes()
	if err != nil {
		span.RecordError(err)
		if err == redis.Nil {
			return nil, fmt.Errorf("conversation: unknown conversation %s", conversationID)
		}
		return nil, fmt.Errorf("conversation: failed to load history: %w", err)
	}

	var history []ChatMessage
	if err := json.Unmarshal(data, &history); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("conversation: failed to decode history: %w", err)
	}
	return history, nil
}

func conversationKey(id string) string {
	return fmt.Sprintf("conversation:%s", id)
}

func timeSelectionKey(conversationID string) string {
	return fmt.Sprintf("time_selection:%s", conversationID)
}

// SaveTimeSelectionState persists the time selection state for a conversation.
func (s *historyStore) SaveTimeSelectionState(ctx context.Context, conversationID string, state *TimeSelectionState) error {
	ctx, span := s.tracer.Start(ctx, "conversation.save_time_selection")
	defer span.End()

	if state == nil {
		// Delete the key if state is nil
		if err := s.redis.Del(ctx, timeSelectionKey(conversationID)).Err(); err != nil {
			span.RecordError(err)
			return fmt.Errorf("conversation: failed to delete time selection state: %w", err)
		}
		return nil
	}

	data, err := json.Marshal(state)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("conversation: failed to marshal time selection state: %w", err)
	}
	if err := s.redis.Set(ctx, timeSelectionKey(conversationID), data, conversationTTL).Err(); err != nil {
		span.RecordError(err)
		return fmt.Errorf("conversation: failed to persist time selection state: %w", err)
	}
	return nil
}

// LoadTimeSelectionState retrieves the time selection state for a conversation.
func (s *historyStore) LoadTimeSelectionState(ctx context.Context, conversationID string) (*TimeSelectionState, error) {
	ctx, span := s.tracer.Start(ctx, "conversation.load_time_selection")
	defer span.End()

	data, err := s.redis.Get(ctx, timeSelectionKey(conversationID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // No state stored
		}
		span.RecordError(err)
		return nil, fmt.Errorf("conversation: failed to load time selection state: %w", err)
	}

	var state TimeSelectionState
	if err := json.Unmarshal(data, &state); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("conversation: failed to decode time selection state: %w", err)
	}
	return &state, nil
}
