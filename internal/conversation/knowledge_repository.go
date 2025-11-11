package conversation

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

const knowledgeKeyPrefix = "rag:docs:"

// KnowledgeRepository persists clinic knowledge snippets.
type KnowledgeRepository interface {
	AppendDocuments(ctx context.Context, clinicID string, docs []string) error
	GetDocuments(ctx context.Context, clinicID string) ([]string, error)
	LoadAll(ctx context.Context) (map[string][]string, error)
}

// RedisKnowledgeRepository stores raw documents in Redis lists.
type RedisKnowledgeRepository struct {
	client *redis.Client
}

// NewRedisKnowledgeRepository creates a Redis-backed knowledge repo.
func NewRedisKnowledgeRepository(client *redis.Client) *RedisKnowledgeRepository {
	if client == nil {
		panic("conversation: redis client cannot be nil")
	}
	return &RedisKnowledgeRepository{client: client}
}

// AppendDocuments pushes new snippets onto the clinic's list.
func (r *RedisKnowledgeRepository) AppendDocuments(ctx context.Context, clinicID string, docs []string) error {
	if len(docs) == 0 {
		return nil
	}
	args := make([]interface{}, len(docs))
	for i, d := range docs {
		args[i] = d
	}
	key := knowledgeKey(clinicID)
	if err := r.client.RPush(ctx, key, args...).Err(); err != nil {
		return fmt.Errorf("conversation: failed to push knowledge: %w", err)
	}
	return nil
}

// GetDocuments retrieves all snippets for the clinic.
func (r *RedisKnowledgeRepository) GetDocuments(ctx context.Context, clinicID string) ([]string, error) {
	return r.client.LRange(ctx, knowledgeKey(clinicID), 0, -1).Result()
}

// LoadAll returns all clinic docs keyed by clinicID.
func (r *RedisKnowledgeRepository) LoadAll(ctx context.Context) (map[string][]string, error) {
	var cursor uint64
	result := make(map[string][]string)

	for {
		keys, next, err := r.client.Scan(ctx, cursor, knowledgeKeyPrefix+"*", 50).Result()
		if err != nil {
			return nil, fmt.Errorf("conversation: scan knowledge keys failed: %w", err)
		}
		for _, key := range keys {
			clinicID := strings.TrimPrefix(key, knowledgeKeyPrefix)
			docs, err := r.client.LRange(ctx, key, 0, -1).Result()
			if err != nil {
				return nil, fmt.Errorf("conversation: fetch knowledge %s failed: %w", clinicID, err)
			}
			result[clinicID] = docs
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return result, nil
}

func knowledgeKey(clinicID string) string {
	return knowledgeKeyPrefix + clinicID
}
