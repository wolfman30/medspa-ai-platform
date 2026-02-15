package conversation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const structuredKnowledgeKeyPrefix = "knowledge:structured:"

// StructuredKnowledgeStore persists structured knowledge in Redis.
type StructuredKnowledgeStore struct {
	client *redis.Client
}

// NewStructuredKnowledgeStore creates a new store.
func NewStructuredKnowledgeStore(client *redis.Client) *StructuredKnowledgeStore {
	return &StructuredKnowledgeStore{client: client}
}

func (s *StructuredKnowledgeStore) key(orgID string) string {
	return structuredKnowledgeKeyPrefix + orgID
}

// GetStructured retrieves structured knowledge for an org.
func (s *StructuredKnowledgeStore) GetStructured(ctx context.Context, orgID string) (*StructuredKnowledge, error) {
	data, err := s.client.Get(ctx, s.key(orgID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("structured knowledge: get: %w", err)
	}
	var sk StructuredKnowledge
	if err := json.Unmarshal(data, &sk); err != nil {
		return nil, fmt.Errorf("structured knowledge: unmarshal: %w", err)
	}
	return &sk, nil
}

// SetStructured saves structured knowledge for an org.
func (s *StructuredKnowledgeStore) SetStructured(ctx context.Context, orgID string, sk *StructuredKnowledge) error {
	data, err := json.Marshal(sk)
	if err != nil {
		return fmt.Errorf("structured knowledge: marshal: %w", err)
	}
	if err := s.client.Set(ctx, s.key(orgID), data, 0).Err(); err != nil {
		return fmt.Errorf("structured knowledge: set: %w", err)
	}
	return nil
}
