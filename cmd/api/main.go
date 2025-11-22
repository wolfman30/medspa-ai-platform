package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	"github.com/wolfman30/medspa-ai-platform/internal/api/router"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/bookings"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	observemetrics "github.com/wolfman30/medspa-ai-platform/internal/observability/metrics"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func main() {
	// Load configuration
	cfg := appconfig.Load()

	// Initialize logger
	logger := logging.New(cfg.LogLevel)
	logger.Info("starting medspa-ai-platform API server",
		"env", cfg.Env,
		"port", cfg.Port,
	)
	logger.Debug("telnyx config loaded",
		"has_api_key", cfg.TelnyxAPIKey != "",
		"has_profile_id", cfg.TelnyxMessagingProfileID != "",
		"has_webhook_secret", cfg.TelnyxWebhookSecret != "",
	)

	registry := prometheus.NewRegistry()
	messagingMetrics := observemetrics.NewMessagingMetrics(registry)
	metricsHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})

	appCtx, stop := context.WithCancel(context.Background())
	defer stop()

	var dbPool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		ctx, cancel := context.WithTimeout(appCtx, 5*time.Second)
		defer cancel()
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("failed to connect to postgres", "error", err)
			os.Exit(1)
		}
		if err := pool.Ping(ctx); err != nil {
			logger.Error("failed to ping postgres", "error", err)
			os.Exit(1)
		}
		dbPool = pool
		defer dbPool.Close()
		logger.Info("connected to postgres")
	}

	// Initialize repositories and services
	var leadsRepo leads.Repository
	if dbPool != nil {
		leadsRepo = leads.NewPostgresRepository(dbPool)
	} else {
		leadsRepo = leads.NewInMemoryRepository()
	}

	var msgStore *messaging.Store
	if dbPool != nil {
		msgStore = messaging.NewStore(dbPool)
	}
	var (
		conversationPublisher *conversation.Publisher
		jobRecorder           conversation.JobRecorder
		jobUpdater            conversation.JobUpdater
		memoryQueue           *conversation.MemoryQueue
	)

	if cfg.UseMemoryQueue {
		if dbPool == nil {
			logger.Error("USE_MEMORY_QUEUE requires DATABASE_URL for job persistence")
			os.Exit(1)
		}
		memoryQueue = conversation.NewMemoryQueue(1024)
		pgStore := conversation.NewPGJobStore(dbPool)
		jobRecorder = pgStore
		jobUpdater = pgStore
		conversationPublisher = conversation.NewPublisher(memoryQueue, pgStore, logger)
	} else {
		awsCfg, err := mainconfig.LoadAWSConfig(appCtx, cfg)
		if err != nil {
			logger.Error("failed to load AWS config", "error", err)
			os.Exit(1)
		}
		sqsClient := sqs.NewFromConfig(awsCfg)
		sqsQueue := conversation.NewSQSQueue(sqsClient, cfg.ConversationQueueURL)
		dynamoClient := dynamodb.NewFromConfig(awsCfg)
		store := conversation.NewJobStore(dynamoClient, cfg.ConversationJobsTable, logger)
		jobRecorder = store
		jobUpdater = store
		conversationPublisher = conversation.NewPublisher(sqsQueue, store, logger)
	}

	// Initialize handlers
	leadsHandler := leads.NewHandler(leadsRepo, logger)
	orgRouting := map[string]string{}
	if raw := strings.TrimSpace(cfg.TwilioOrgMapJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &orgRouting); err != nil {
			logger.Warn("failed to parse TWILIO_ORG_MAP_JSON", "error", err)
		}
	}
	if len(orgRouting) == 0 {
		logger.Warn("TWILIO_ORG_MAP_JSON empty; SMS webhooks will be rejected unless numbers are configured")
	}
	resolver := messaging.NewStaticOrgResolver(orgRouting)
	twilioWebhookSecret := cfg.TwilioWebhookSecret
	if twilioWebhookSecret == "" {
		twilioWebhookSecret = cfg.TwilioAuthToken
	}
	messengerCfg := messaging.ProviderSelectionConfig{
		Preference:       cfg.SMSProvider,
		TelnyxAPIKey:     cfg.TelnyxAPIKey,
		TelnyxProfileID:  cfg.TelnyxMessagingProfileID,
		TwilioAccountSID: cfg.TwilioAccountSID,
		TwilioAuthToken:  cfg.TwilioAuthToken,
		TwilioFromNumber: cfg.TwilioFromNumber,
	}
	webhookMessenger, webhookMessengerProvider, webhookMessengerReason := messaging.BuildReplyMessenger(messengerCfg, logger)
	if webhookMessenger != nil {
		logger.Info("sms messenger initialized for webhooks",
			"provider", webhookMessengerProvider,
			"preference", cfg.SMSProvider,
		)
	} else {
		logger.Warn("sms replies disabled for webhooks",
			"preference", cfg.SMSProvider,
			"reason", webhookMessengerReason,
		)
	}
	messagingHandler := messaging.NewHandler(twilioWebhookSecret, conversationPublisher, resolver, webhookMessenger, logger)

	var telnyxClient *telnyxclient.Client
	if cfg.TelnyxAPIKey != "" {
		logger.Debug("creating telnyx client", "profile_id", cfg.TelnyxMessagingProfileID)
		client, err := telnyxclient.New(telnyxclient.Config{
			APIKey:        cfg.TelnyxAPIKey,
			WebhookSecret: cfg.TelnyxWebhookSecret,
			Timeout:       10 * time.Second,
			Logger:        logger.Logger,
		})
		if err != nil {
			logger.Error("failed to configure telnyx client", "error", err)
			os.Exit(1)
		}
		telnyxClient = client
		logger.Debug("telnyx client created successfully")
	} else {
		logger.Debug("telnyx client not created: API key empty")
	}

	var quietHours compliance.QuietHours
	quietHoursEnabled := false
	if cfg.QuietHoursStart != "" && cfg.QuietHoursEnd != "" {
		if parsed, err := compliance.ParseQuietHours(cfg.QuietHoursStart, cfg.QuietHoursEnd, cfg.QuietHoursTimezone); err != nil {
			logger.Warn("invalid quiet hours configuration", "error", err)
		} else {
			quietHours = parsed
			quietHoursEnabled = true
		}
	}

	var adminMessagingHandler *handlers.AdminMessagingHandler
	if msgStore != nil && telnyxClient != nil {
		adminMessagingHandler = handlers.NewAdminMessagingHandler(handlers.AdminMessagingConfig{
			Store:             msgStore,
			Logger:            logger,
			Telnyx:            telnyxClient,
			QuietHours:        quietHours,
			QuietHoursEnabled: quietHoursEnabled,
			MessagingProfile:  cfg.TelnyxMessagingProfileID,
			StopAck:           cfg.TelnyxStopReply,
			HelpAck:           cfg.TelnyxHelpReply,
			RetryBaseDelay:    cfg.TelnyxRetryBaseDelay,
			Metrics:           messagingMetrics,
		})
	}
	var paymentsRepo *payments.Repository
	var outboxStore *events.OutboxStore
	var processedStore *events.ProcessedStore
	if dbPool != nil {
		paymentsRepo = payments.NewRepository(dbPool)
		outboxStore = events.NewOutboxStore(dbPool)
		processedStore = events.NewProcessedStore(dbPool)
	}

	var knowledgeRepo conversation.KnowledgeRepository
	var ragIngestor conversation.RAGIngestor
	if cfg.RedisAddr != "" && cfg.OpenAIEmbeddingModel != "" && cfg.OpenAIAPIKey != "" {
		redisClient := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		})
		knowledgeRepo = conversation.NewRedisKnowledgeRepository(redisClient)

		openaiCfg := openai.DefaultConfig(cfg.OpenAIAPIKey)
		if cfg.OpenAIBaseURL != "" {
			openaiCfg.BaseURL = cfg.OpenAIBaseURL
		}
		openaiClient := openai.NewClientWithConfig(openaiCfg)
		ragIngestor = conversation.NewMemoryRAGStore(openaiClient, cfg.OpenAIEmbeddingModel, logger)
	}
	conversationHandler := conversation.NewHandler(conversationPublisher, jobRecorder, knowledgeRepo, ragIngestor, logger)

	var inlineWorker *conversation.Worker
	if cfg.UseMemoryQueue {
		processor, err := appbootstrap.BuildConversationService(appCtx, cfg, logger)
		if err != nil {
			logger.Error("failed to configure inline conversation service", "error", err)
			os.Exit(1)
		}

		var (
			messenger       conversation.ReplyMessenger
			messengerReason string
		)
		messenger, _, messengerReason = webhookMessenger, webhookMessengerProvider, webhookMessengerReason
		if messenger == nil {
			logger.Warn("no sms credentials configured; SMS replies disabled for inline workers",
				"preference", cfg.SMSProvider,
				"reason", messengerReason,
			)
		}

		var bookingBridge conversation.BookingServiceAdapter
		if dbPool != nil {
			repo := bookings.NewRepository(dbPool)
			bookingBridge = conversation.BookingServiceAdapter{
				Service: bookings.NewService(repo, logger),
			}
		}

		inlineWorker = conversation.NewWorker(
			processor,
			memoryQueue,
			jobUpdater,
			messenger,
			bookingBridge,
			logger,
			conversation.WithWorkerCount(cfg.WorkerCount),
		)
		inlineWorker.Start(appCtx)
		logger.Info("inline conversation workers started", "count", cfg.WorkerCount)
	}

	var checkoutHandler *payments.CheckoutHandler
	var squareWebhookHandler *payments.SquareWebhookHandler
	if paymentsRepo != nil && processedStore != nil && outboxStore != nil {
		squareSvc := payments.NewSquareCheckoutService(cfg.SquareAccessToken, cfg.SquareLocationID, cfg.SquareSuccessURL, cfg.SquareCancelURL, logger)
		checkoutHandler = payments.NewCheckoutHandler(leadsRepo, paymentsRepo, squareSvc, logger, int32(cfg.DepositAmountCents))
		squareWebhookHandler = payments.NewSquareWebhookHandler(cfg.SquareWebhookKey, paymentsRepo, leadsRepo, processedStore, outboxStore, resolver, logger)
		dispatcher := conversation.NewOutboxDispatcher(conversationPublisher)
		deliverer := events.NewDeliverer(outboxStore, dispatcher, logger)
		go deliverer.Start(appCtx)
	}

	var telnyxWebhookHandler *handlers.TelnyxWebhookHandler
	logger.Debug("checking telnyx webhook handler prerequisites",
		"msgStore", msgStore != nil,
		"telnyxClient", telnyxClient != nil,
		"processedStore", processedStore != nil,
	)
	if msgStore != nil && telnyxClient != nil && processedStore != nil {
		logger.Debug("creating telnyx webhook handler")
		telnyxWebhookHandler = handlers.NewTelnyxWebhookHandler(handlers.TelnyxWebhookConfig{
			Store:            msgStore,
			Processed:        processedStore,
			Telnyx:           telnyxClient,
			Conversation:     conversationPublisher,
			Logger:           logger,
			MessagingProfile: cfg.TelnyxMessagingProfileID,
			StopAck:          cfg.TelnyxStopReply,
			HelpAck:          cfg.TelnyxHelpReply,
			Metrics:          messagingMetrics,
		})
		logger.Info("telnyx webhook handler initialized", "profile_id", cfg.TelnyxMessagingProfileID)
	} else {
		logger.Warn("telnyx webhook handler NOT created - missing prerequisites")
	}

	// Setup router
	routerCfg := &router.Config{
		Logger:              logger,
		LeadsHandler:        leadsHandler,
		MessagingHandler:    messagingHandler,
		ConversationHandler: conversationHandler,
		PaymentsHandler:     checkoutHandler,
		SquareWebhook:       squareWebhookHandler,
		AdminMessaging:      adminMessagingHandler,
		TelnyxWebhooks:      telnyxWebhookHandler,
		AdminAuthSecret:     cfg.AdminJWTSecret,
		MetricsHandler:      metricsHandler,
	}
	r := router.New(routerCfg)

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	stop()
	logger.Info("shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	if inlineWorker != nil {
		waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer waitCancel()
		done := make(chan struct{})
		go func() {
			inlineWorker.Wait()
			close(done)
		}()

		select {
		case <-done:
			logger.Info("inline conversation workers stopped")
		case <-waitCtx.Done():
			logger.Warn("inline conversation workers shutdown timed out", "error", waitCtx.Err())
		}
	}

	logger.Info("server stopped")
	fmt.Println("Server exited gracefully")
}
