package bootstrap

import (
	"context"
	"crypto/tls"
	"database/sql"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// BuildRedisClient returns a configured Redis client or nil when disabled.
// When verify is true, a ping is issued and failures return nil.
func BuildRedisClient(ctx context.Context, cfg *appconfig.Config, logger *logging.Logger, verify bool) *redis.Client {
	if cfg == nil || strings.TrimSpace(cfg.RedisAddr) == "" {
		return nil
	}
	if logger == nil {
		logger = logging.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	redisOptions := &redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	}
	if cfg.RedisTLS {
		redisOptions.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	client := redis.NewClient(redisOptions)
	if !verify {
		return client
	}
	if err := client.Ping(ctx).Err(); err != nil {
		logger.Warn("redis not available", "error", err)
		return nil
	}
	return client
}

// BuildConversationStore wires optional conversation persistence with exclusions.
func BuildConversationStore(sqlDB *sql.DB, cfg *appconfig.Config, logger *logging.Logger, logEnabled bool) *conversation.ConversationStore {
	if cfg == nil || !cfg.PersistConversationHistory || sqlDB == nil {
		return nil
	}
	if logger == nil {
		logger = logging.Default()
	}

	excludePhones := parseConversationExclusions(cfg.ConversationPersistExcludePhone)
	if len(excludePhones) > 0 {
		store := conversation.NewConversationStoreWithExclusions(sqlDB, excludePhones)
		logger.Info("conversation persistence enabled with exclusions", "excluded_count", len(excludePhones))
		return store
	}

	if logEnabled {
		logger.Info("conversation persistence enabled")
	}
	return conversation.NewConversationStore(sqlDB)
}

// BuildClinicStore returns the clinic config store when Redis is available.
func BuildClinicStore(redisClient *redis.Client) *clinic.Store {
	if redisClient == nil {
		return nil
	}
	return clinic.NewStore(redisClient)
}

// BuildSMSTranscriptStore returns the Redis-backed SMS transcript store.
func BuildSMSTranscriptStore(redisClient *redis.Client) *conversation.SMSTranscriptStore {
	return conversation.NewSMSTranscriptStore(redisClient)
}

func parseConversationExclusions(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var excludePhones []string
	for _, p := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			excludePhones = append(excludePhones, trimmed)
		}
	}
	return excludePhones
}
