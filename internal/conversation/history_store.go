package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
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

func (s *historyStore) Save(ctx context.Context, conversationID string, history []openai.ChatCompletionMessage) error {
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

func (s *historyStore) Load(ctx context.Context, conversationID string) ([]openai.ChatCompletionMessage, error) {
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

	var history []openai.ChatCompletionMessage
	if err := json.Unmarshal(data, &history); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("conversation: failed to decode history: %w", err)
	}
	return history, nil
}

func conversationKey(id string) string {
	return fmt.Sprintf("conversation:%s", id)
}
