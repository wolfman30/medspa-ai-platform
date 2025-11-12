package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	"github.com/wolfman30/medspa-ai-platform/internal/worker/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func main() {
	cfg := config.Load()
	logger := logging.New(cfg.LogLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.DatabaseURL == "" || cfg.TelnyxAPIKey == "" {
		logger.Error("messaging worker requires DATABASE_URL and TELNYX_API_KEY")
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	telnyxClient, err := telnyxclient.New(telnyxclient.Config{
		APIKey:        cfg.TelnyxAPIKey,
		WebhookSecret: cfg.TelnyxWebhookSecret,
		Timeout:       10 * time.Second,
		Logger:        logger.Logger,
	})
	if err != nil {
		logger.Error("failed to create telnyx client", "error", err)
		os.Exit(1)
	}

	store := messaging.NewStore(pool)

	retryInterval := cfg.TelnyxRetryBaseDelay / 2
	if retryInterval <= 0 {
		retryInterval = time.Minute
	}
	retry := messagingworker.NewRetrySender(store, telnyxClient, logger).
		WithMaxAttempts(cfg.TelnyxRetryMaxAttempts).
		WithBaseDelay(cfg.TelnyxRetryBaseDelay).
		WithInterval(retryInterval)

	hosted := messagingworker.NewHostedPoller(store, telnyxClient, logger).
		WithInterval(cfg.TelnyxHostedPollInterval)

	go retry.Run(ctx)
	go hosted.Run(ctx)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("messaging worker shutting down")
	cancel()
	time.Sleep(2 * time.Second)
}
