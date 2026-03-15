package bootstrap

import (
	"context"

	"github.com/redis/go-redis/v9"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// ClinicBootstrap holds Redis-backed clinic configuration services.
type ClinicBootstrap struct {
	RedisClient   *redis.Client
	ClinicStore   *clinic.Store
	SMSTranscript *conversation.SMSTranscriptStore
}

// BootstrapClinic connects to Redis and initializes the clinic config store
// and SMS transcript store.
func BootstrapClinic(cfg *appconfig.Config, appCtx context.Context, logger *logging.Logger) ClinicBootstrap {
	redisClient := appbootstrap.BuildRedisClient(appCtx, cfg, logger, false)
	clinicStore := appbootstrap.BuildClinicStore(redisClient)
	smsTranscript := appbootstrap.BuildSMSTranscriptStore(redisClient)
	return ClinicBootstrap{RedisClient: redisClient, ClinicStore: clinicStore, SMSTranscript: smsTranscript}
}

// BootstrapNotifications creates the GitHub webhook handler that forwards
// CI/CD events to Telegram. Returns nil if GITHUB_WEBHOOK_SECRET is not set.
func BootstrapNotifications(cfg *appconfig.Config, logger *logging.Logger) *handlers.GitHubWebhookHandler {
	var githubWebhookHandler *handlers.GitHubWebhookHandler
	if cfg.GitHubWebhookSecret != "" {
		githubNotifier := handlers.NewTelegramNotifier(cfg.TelegramBotToken, cfg.AndrewTelegramChatID, logger)
		githubWebhookHandler = handlers.NewGitHubWebhookHandler(cfg.GitHubWebhookSecret, githubNotifier, logger)
		logger.Info("github webhook handler initialized")
	} else {
		logger.Warn("github webhook handler not initialized (GITHUB_WEBHOOK_SECRET missing)")
	}
	return githubWebhookHandler
}
