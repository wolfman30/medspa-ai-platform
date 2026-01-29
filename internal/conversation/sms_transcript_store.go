package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const smsTranscriptKeyPrefix = "sms_transcript:"

type SMSTranscriptMessage struct {
	ID                string            `json:"id"`
	Role              string            `json:"role"` // "user" or "assistant"
	From              string            `json:"from"`
	To                string            `json:"to"`
	Body              string            `json:"body"`
	Timestamp         time.Time         `json:"timestamp"`
	Kind              string            `json:"kind,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	ProviderMessageID string            `json:"provider_message_id,omitempty"`
	Status            string            `json:"status,omitempty"`
	ErrorReason       string            `json:"error_reason,omitempty"`
}

type SMSTranscriptStore struct {
	redis       *redis.Client
	tracer      trace.Tracer
	maxMessages int64
}

func NewSMSTranscriptStore(redisClient *redis.Client) *SMSTranscriptStore {
	if redisClient == nil {
		return nil
	}
	return &SMSTranscriptStore{
		redis:       redisClient,
		tracer:      otel.Tracer("medspa.internal.conversation.sms_transcript"),
		maxMessages: 250,
	}
}

func (s *SMSTranscriptStore) Append(ctx context.Context, conversationID string, msg SMSTranscriptMessage) error {
	if s == nil || s.redis == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if conversationID == "" {
		return errors.New("conversation: sms transcript conversationID required")
	}

	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("conversation: marshal sms transcript message: %w", err)
	}

	ctx, span := s.tracer.Start(ctx, "conversation.sms_transcript.append")
	defer span.End()

	key := smsTranscriptKey(conversationID)
	pipe := s.redis.TxPipeline()
	pipe.RPush(ctx, key, data)
	pipe.Expire(ctx, key, conversationTTL)
	if s.maxMessages > 0 {
		pipe.LTrim(ctx, key, -s.maxMessages, -1)
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("conversation: append sms transcript message: %w", err)
	}
	return nil
}

func (s *SMSTranscriptStore) List(ctx context.Context, conversationID string, limit int64) ([]SMSTranscriptMessage, error) {
	if s == nil || s.redis == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if conversationID == "" {
		return nil, errors.New("conversation: sms transcript conversationID required")
	}

	ctx, span := s.tracer.Start(ctx, "conversation.sms_transcript.list")
	defer span.End()

	start := int64(0)
	end := int64(-1)
	if limit > 0 {
		start = -limit
	}

	key := smsTranscriptKey(conversationID)
	raw, err := s.redis.LRange(ctx, key, start, end).Result()
	if err != nil {
		span.RecordError(err)
		if err == redis.Nil {
			return []SMSTranscriptMessage{}, nil
		}
		return nil, fmt.Errorf("conversation: list sms transcript: %w", err)
	}

	out := make([]SMSTranscriptMessage, 0, len(raw))
	for _, item := range raw {
		var msg SMSTranscriptMessage
		if err := json.Unmarshal([]byte(item), &msg); err != nil {
			span.RecordError(err)
			continue
		}
		out = append(out, msg)
	}
	return out, nil
}

// HasAssistantMessage returns true if any assistant message exists in the transcript list.
func (s *SMSTranscriptStore) HasAssistantMessage(ctx context.Context, conversationID string) (bool, error) {
	if s == nil || s.redis == nil {
		return false, nil
	}
	messages, err := s.List(ctx, conversationID, 0)
	if err != nil {
		return false, err
	}
	for _, msg := range messages {
		if msg.Role == "assistant" {
			return true, nil
		}
	}
	return false, nil
}

func smsTranscriptKey(conversationID string) string {
	return smsTranscriptKeyPrefix + conversationID
}
