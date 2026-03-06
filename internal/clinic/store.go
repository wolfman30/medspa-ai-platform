package clinic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Store provides persistence for clinic configurations.
type Store struct {
	redis *redis.Client
}

// NewStore creates a new clinic config store.
func NewStore(redisClient *redis.Client) *Store {
	return &Store{redis: redisClient}
}

func (s *Store) key(orgID string) string {
	return fmt.Sprintf("clinic:config:%s", orgID)
}

// Get retrieves clinic config, returning default if not found.
func (s *Store) Get(ctx context.Context, orgID string) (*Config, error) {
	data, err := s.redis.Get(ctx, s.key(orgID)).Bytes()
	if err == redis.Nil {
		return DefaultConfig(orgID), nil
	}
	if err != nil {
		return nil, fmt.Errorf("clinic: get config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("clinic: unmarshal config: %w", err)
	}

	return &cfg, nil
}

// GetStripeAccountID retrieves the Stripe account ID for a clinic.
func (s *Store) GetStripeAccountID(ctx context.Context, orgID string) (string, error) {
	cfg, err := s.Get(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("clinic: get stripe account: %w", err)
	}
	return cfg.StripeAccountID, nil
}

// SaveStripeAccountID updates the clinic's Stripe account ID and sets the payment provider to "stripe".
// This satisfies the payments.StripeConfigSaver interface for Stripe Connect onboarding.
func (s *Store) SaveStripeAccountID(ctx context.Context, orgID, accountID string) error {
	cfg, err := s.Get(ctx, orgID)
	if err != nil {
		return fmt.Errorf("clinic: save stripe account: get: %w", err)
	}
	cfg.StripeAccountID = accountID
	cfg.PaymentProvider = "stripe"
	return s.Set(ctx, cfg)
}

// Set saves clinic config.
func (s *Store) Set(ctx context.Context, cfg *Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("clinic: marshal config: %w", err)
	}

	if err := s.redis.Set(ctx, s.key(cfg.OrgID), data, 0).Err(); err != nil {
		return fmt.Errorf("clinic: set config: %w", err)
	}

	return nil
}
