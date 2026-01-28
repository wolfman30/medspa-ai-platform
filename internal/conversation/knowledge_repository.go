package conversation

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

const knowledgeKeyPrefix = "rag:docs:"
const knowledgeVersionKeyPrefix = "rag:docs:ver:"

// KnowledgeRepository persists clinic knowledge snippets.
type KnowledgeRepository interface {
	AppendDocuments(ctx context.Context, clinicID string, docs []string) error
	GetDocuments(ctx context.Context, clinicID string) ([]string, error)
	LoadAll(ctx context.Context) (map[string][]string, error)
}

// KnowledgeReplacer replaces clinic knowledge documents.
type KnowledgeReplacer interface {
	ReplaceDocuments(ctx context.Context, clinicID string, docs []string) error
}

// KnowledgeVersioner tracks knowledge versions per clinic.
type KnowledgeVersioner interface {
	GetVersion(ctx context.Context, clinicID string) (int64, error)
	SetVersion(ctx context.Context, clinicID string, version int64) error
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

// ReplaceDocuments overwrites all snippets for the clinic.
func (r *RedisKnowledgeRepository) ReplaceDocuments(ctx context.Context, clinicID string, docs []string) error {
	key := knowledgeKey(clinicID)
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, key)
	if len(docs) > 0 {
		args := make([]interface{}, len(docs))
		for i, d := range docs {
			args[i] = d
		}
		pipe.RPush(ctx, key, args...)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("conversation: failed to replace knowledge: %w", err)
	}
	return nil
}

// GetDocuments retrieves all snippets for the clinic.
func (r *RedisKnowledgeRepository) GetDocuments(ctx context.Context, clinicID string) ([]string, error) {
	return r.client.LRange(ctx, knowledgeKey(clinicID), 0, -1).Result()
}

// GetVersion retrieves the version for the clinic knowledge.
func (r *RedisKnowledgeRepository) GetVersion(ctx context.Context, clinicID string) (int64, error) {
	val, err := r.client.Get(ctx, knowledgeVersionKey(clinicID)).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, fmt.Errorf("conversation: get knowledge version: %w", err)
	}
	version, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("conversation: parse knowledge version: %w", err)
	}
	return version, nil
}

// SetVersion stores the version for the clinic knowledge.
func (r *RedisKnowledgeRepository) SetVersion(ctx context.Context, clinicID string, version int64) error {
	if err := r.client.Set(ctx, knowledgeVersionKey(clinicID), strconv.FormatInt(version, 10), 0).Err(); err != nil {
		return fmt.Errorf("conversation: set knowledge version: %w", err)
	}
	return nil
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

func knowledgeVersionKey(clinicID string) string {
	return knowledgeVersionKeyPrefix + clinicID
}
