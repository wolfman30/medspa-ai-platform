package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	"github.com/wolfman30/medspa-ai-platform/internal/api/router"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/archive"
	"github.com/wolfman30/medspa-ai-platform/internal/bookings"
	"github.com/wolfman30/medspa-ai-platform/internal/briefs"
	"github.com/wolfman30/medspa-ai-platform/internal/browser"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/clinicdata"
	auditcompliance "github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/notify"
	observemetrics "github.com/wolfman30/medspa-ai-platform/internal/observability/metrics"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/internal/prospects"
	"github.com/wolfman30/medspa-ai-platform/migrations"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"

	"github.com/golang-migrate/migrate/v4"
	pgmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

func main() {
	// Load .env file if it exists
	_ = godotenv.Load()

	// Load configuration
	cfg := appconfig.Load()

	// Initialize logger
	logger := logging.New(cfg.LogLevel)
	logger.Info("starting medspa-ai-platform API server",
		"env", cfg.Env,
		"port", cfg.Port,
	)
	// Validate SMS provider configuration at startup
	if issues := cfg.SMSProviderIssues(); len(issues) > 0 {
		for _, issue := range issues {
			logger.Error("SMS PROVIDER MISCONFIGURATION", "issue", issue)
		}
		logger.Error("voice-to-SMS acknowledgements will NOT work — check AWS Secrets Manager")
	}

	logger.Debug("telnyx config loaded",
		"has_api_key", cfg.TelnyxAPIKey != "",
		"has_profile_id", cfg.TelnyxMessagingProfileID != "",
		"has_webhook_secret", cfg.TelnyxWebhookSecret != "",
	)

	metricsHandler, messagingMetrics := setupMessagingMetrics()

	// Setup application context
	appCtx, stop := context.WithCancel(context.Background())
	defer stop()

	dbPool := connectPostgresPool(appCtx, cfg.DatabaseURL, logger)
	if dbPool != nil {
		defer dbPool.Close()
	}
	sqlDB := connectSQLDB(dbPool, logger)
	if sqlDB != nil {
		defer sqlDB.Close()
		runAutoMigrate(sqlDB, logger)
	}
	conversationStore := appbootstrap.BuildConversationStore(sqlDB, cfg, logger, true)
	var auditSvc *auditcompliance.AuditService
	if sqlDB != nil {
		auditSvc = auditcompliance.NewAuditService(sqlDB)
	}

	// Initialize repositories and services
	leadsRepo := initializeLeadsRepository(dbPool)

	msgStore := messaging.NewStore(dbPool)

	conversationPublisher, jobRecorder, jobUpdater, memoryQueue := setupConversation(appCtx, cfg, dbPool, logger)

	// Create Redis client for knowledge repo and clinic config
	redisClient := appbootstrap.BuildRedisClient(appCtx, cfg, logger, false)
	clinicStore := appbootstrap.BuildClinicStore(redisClient)
	smsTranscript := appbootstrap.BuildSMSTranscriptStore(redisClient)

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
	webhookMessenger, webhookMessengerProvider, webhookMessengerReason := appbootstrap.BuildOutboundMessenger(
		cfg,
		logger,
		msgStore,
		auditSvc,
		conversationStore,
		smsTranscript,
	)
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
	messagingHandler := messaging.NewHandler(twilioWebhookSecret, conversationPublisher, resolver, webhookMessenger, leadsRepo, logger)
	messagingHandler.SetConversationStore(conversationStore)
	messagingHandler.SetClinicStore(clinicStore)
	messagingHandler.SetPublicBaseURL(cfg.PublicBaseURL)
	messagingHandler.SetSkipSignature(cfg.TwilioSkipSignature)

	// Production safety check: warn loudly if signature validation is disabled
	if cfg.TwilioSkipSignature && (cfg.Env == "production" || cfg.Env == "staging") {
		logger.Error("SECURITY WARNING: TWILIO_SKIP_SIGNATURE is enabled in production/staging - this is a security risk!")
	}

	telnyxClient := setupTelnyxClient(cfg, logger)

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
		paymentsRepo = payments.NewRepository(dbPool, redisClient)
		outboxStore = events.NewOutboxStore(dbPool)
		processedStore = events.NewProcessedStore(dbPool)
	}

	var clinicHandler *clinic.Handler
	var clinicStatsHandler *clinic.StatsHandler
	var clinicDashboardHandler *clinic.DashboardHandler
	if clinicStore != nil {
		clinicHandler = clinic.NewHandler(clinicStore, logger)
	}
	if dbPool != nil {
		statsRepo := clinic.NewStatsRepository(dbPool)
		clinicStatsHandler = clinic.NewStatsHandler(statsRepo, logger)

		dashboardRepo := clinic.NewDashboardRepository(dbPool)
		clinicDashboardHandler = clinic.NewDashboardHandler(dashboardRepo, prometheus.DefaultGatherer, logger)
	}

	var adminClinicDataHandler *handlers.AdminClinicDataHandler
	if cfg.Env != "production" && dbPool != nil {
		adminCfg := handlers.AdminClinicDataConfig{
			DB:     dbPool,
			Redis:  redisClient,
			Logger: logger,
		}
		// Set up S3 archiver if bucket is configured
		if cfg.S3ArchiveBucket != "" {
			awsCfg, err := mainconfig.LoadAWSConfig(appCtx, cfg)
			if err != nil {
				logger.Warn("failed to load AWS config for archiver, archiving disabled", "error", err)
			} else {
				s3Client := s3.NewFromConfig(awsCfg)
				adminCfg.Archiver = clinicdata.NewArchiver(clinicdata.ArchiverConfig{
					DB:       dbPool,
					S3:       s3Client,
					Bucket:   cfg.S3ArchiveBucket,
					KMSKeyID: cfg.S3ArchiveKMSKey,
					Logger:   logger,
				})
				logger.Info("S3 archiver enabled for admin purge operations",
					"bucket", cfg.S3ArchiveBucket,
					"kms_key", cfg.S3ArchiveKMSKey != "",
				)
			}
		}
		// Set up training archiver if training bucket is configured
		if cfg.S3TrainingBucket != "" {
			awsCfg, awsErr := mainconfig.LoadAWSConfig(appCtx, cfg)
			if awsErr != nil {
				logger.Warn("failed to load AWS config for training archiver", "error", awsErr)
			} else {
				trainingS3 := s3.NewFromConfig(awsCfg)
				brClient := bedrockruntime.NewFromConfig(awsCfg)

				trainingStore := archive.NewStore(trainingS3, cfg.S3TrainingBucket, logger.Logger)
				classifier := archive.NewClassifier(brClient, cfg.ClassifierModelID, logger.Logger)
				adminCfg.TrainingArchiver = archive.NewTrainingArchiver(trainingStore, classifier, logger.Logger)
				logger.Info("training archiver enabled for purge operations",
					"bucket", cfg.S3TrainingBucket,
					"classifier_model", cfg.ClassifierModelID,
				)
			}
		}
		adminClinicDataHandler = handlers.NewAdminClinicDataHandler(adminCfg)
	}

	// Initialize onboarding handler
	var adminOnboardingHandler *handlers.AdminOnboardingHandler
	if clinicStore != nil {
		adminOnboardingHandler = handlers.NewAdminOnboardingHandler(handlers.AdminOnboardingConfig{
			DB:          dbPool,
			Redis:       redisClient,
			ClinicStore: clinicStore,
			Logger:      logger,
		})
		logger.Info("onboarding handler initialized")
	}

	// Initialize client registration handler (for self-service registration)
	var clientRegistrationHandler *handlers.ClientRegistrationHandler
	if sqlDB != nil {
		clientRegistrationHandler = handlers.NewClientRegistrationHandler(sqlDB, redisClient, logger)
		logger.Info("client registration handler initialized")
	}

	var knowledgeRepo conversation.KnowledgeRepository
	if redisClient != nil {
		knowledgeRepo = conversation.NewRedisKnowledgeRepository(redisClient)
	}
	conversationHandler := conversation.NewHandler(conversationPublisher, jobRecorder, knowledgeRepo, nil, logger)
	conversationHandler.SetSMSTranscriptStore(smsTranscript)

	supervisor, err := appbootstrap.BuildSupervisor(appCtx, cfg, logger)
	if err != nil {
		logger.Error("failed to configure supervisor", "error", err)
		os.Exit(1)
	}

	inlineWorker, conversationService := setupInlineWorker(
		appCtx,
		cfg,
		logger,
		webhookMessenger,
		webhookMessengerReason,
		jobUpdater,
		memoryQueue,
		dbPool,
		sqlDB,
		auditSvc,
		outboxStore,
		resolver,
		msgStore,
		supervisor,
		redisClient,
		smsTranscript,
	)
	if conversationService != nil {
		conversationHandler.SetService(conversationService)
	}

	// Voice AI handler (Telnyx AI Assistant webhook tool)
	var voiceAIHandler *handlers.VoiceAIHandler
	if msgStore != nil && clinicStore != nil {
		voiceAIHandler = handlers.NewVoiceAIHandler(handlers.VoiceAIHandlerConfig{
			Store:       msgStore,
			Publisher:   conversationPublisher,
			Processor:   conversationService,
			ClinicStore: clinicStore,
			Redis:       redisClient,
			Logger:      logger,
		})
		logger.Info("voice AI handler initialized")
	}

	var checkoutHandler *payments.CheckoutHandler
	var squareWebhookHandler *payments.SquareWebhookHandler
	var squareOAuthHandler *payments.OAuthHandler
	var fakePaymentsHandler *payments.FakePaymentsHandler
	if paymentsRepo != nil && processedStore != nil && outboxStore != nil {
		// Square OAuth service for per-clinic payment connections
		var oauthSvc *payments.SquareOAuthService
		// Number resolver for webhook handler - defaults to static, wrapped with DB lookup
		var numberResolver payments.OrgNumberResolver = resolver
		var orderClient interface {
			FetchMetadata(ctx context.Context, orderID string) (map[string]string, error)
		}
		if strings.TrimSpace(cfg.SquareAccessToken) != "" {
			orderClient = payments.NewSquareOrdersClient(cfg.SquareAccessToken, cfg.SquareBaseURL, logger)
		}

		if cfg.SquareClientID != "" && cfg.SquareClientSecret != "" && cfg.SquareOAuthRedirectURI != "" {
			oauthSvc = payments.NewSquareOAuthService(
				payments.SquareOAuthConfig{
					ClientID:     cfg.SquareClientID,
					ClientSecret: cfg.SquareClientSecret,
					RedirectURI:  cfg.SquareOAuthRedirectURI,
					Sandbox:      cfg.SquareSandbox,
				},
				dbPool,
				logger,
			)
			squareOAuthHandler = payments.NewOAuthHandler(oauthSvc, cfg.SquareOAuthSuccessURL, logger)
			logger.Info("square oauth handler initialized", "redirect_uri", cfg.SquareOAuthRedirectURI, "sandbox", cfg.SquareSandbox)

			// Start token refresh worker
			tokenRefreshWorker := payments.NewTokenRefreshWorker(oauthSvc, logger)
			go tokenRefreshWorker.Start(appCtx)

			// Wrap static resolver with DB lookup for phone numbers
			numberResolver = payments.NewDBOrgNumberResolver(oauthSvc, resolver)
		}
		fallbackFromNumber := strings.TrimSpace(cfg.TelnyxFromNumber)
		if fallbackFromNumber == "" {
			fallbackFromNumber = strings.TrimSpace(cfg.TwilioFromNumber)
		}
		if fallbackFromNumber != "" {
			numberResolver = payments.NewFallbackNumberResolver(numberResolver, fallbackFromNumber)
		}

		hasSquareProvider := strings.TrimSpace(cfg.SquareAccessToken) != "" || (cfg.SquareClientID != "" && cfg.SquareClientSecret != "" && cfg.SquareOAuthRedirectURI != "")
		useFakePayments := cfg.AllowFakePayments && !hasSquareProvider
		// Fake payments mode only when Square isn't configured (for testing when sandbox is broken)
		if useFakePayments {
			fakeSvc := payments.NewFakeCheckoutService(cfg.PublicBaseURL, logger)
			checkoutHandler = payments.NewCheckoutHandler(leadsRepo, paymentsRepo, fakeSvc, logger, int32(cfg.DepositAmountCents))
			fakePaymentsHandler = payments.NewFakePaymentsHandler(paymentsRepo, leadsRepo, processedStore, outboxStore, numberResolver, cfg.PublicBaseURL, logger)
			logger.Warn("using fake payments mode (ALLOW_FAKE_PAYMENTS=true)")
		} else {
			usePaymentLinks := payments.UsePaymentLinks(cfg.SquareCheckoutMode, cfg.SquareSandbox)
			logger.Info("square checkout mode configured", "mode", cfg.SquareCheckoutMode, "sandbox", cfg.SquareSandbox, "usePaymentLinks", usePaymentLinks)
			squareSvc := payments.NewSquareCheckoutService(cfg.SquareAccessToken, cfg.SquareLocationID, cfg.SquareSuccessURL, cfg.SquareCancelURL, logger).
				WithBaseURL(cfg.SquareBaseURL).
				WithPaymentLinks(usePaymentLinks).
				WithPaymentLinkFallback(cfg.SquareCheckoutAllowFallback)
			if oauthSvc != nil {
				squareSvc = squareSvc.WithCredentialsProvider(oauthSvc)
			}
			checkoutHandler = payments.NewCheckoutHandler(leadsRepo, paymentsRepo, squareSvc, logger, int32(cfg.DepositAmountCents))
		}

		squareWebhookHandler = payments.NewSquareWebhookHandler(cfg.SquareWebhookKey, paymentsRepo, leadsRepo, processedStore, outboxStore, numberResolver, orderClient, logger)
		dispatcher := conversation.NewOutboxDispatcher(conversationPublisher)
		deliverer := events.NewDeliverer(outboxStore, dispatcher, logger)
		go deliverer.Start(appCtx)
	}

	// Initialize Stripe handlers
	var stripeWebhookHandler *payments.StripeWebhookHandler
	var stripeConnectHandler *payments.StripeConnectHandler
	if cfg.StripeWebhookSecret != "" && paymentsRepo != nil && processedStore != nil && outboxStore != nil {
		var stripeNumberResolver payments.OrgNumberResolver = resolver
		fallbackFromNumber := strings.TrimSpace(cfg.TelnyxFromNumber)
		if fallbackFromNumber == "" {
			fallbackFromNumber = strings.TrimSpace(cfg.TwilioFromNumber)
		}
		if fallbackFromNumber != "" {
			stripeNumberResolver = payments.NewFallbackNumberResolver(stripeNumberResolver, fallbackFromNumber)
		}
		stripeWebhookHandler = payments.NewStripeWebhookHandler(cfg.StripeWebhookSecret, paymentsRepo, leadsRepo, processedStore, outboxStore, stripeNumberResolver, logger)
		logger.Info("stripe webhook handler initialized")
	}
	if cfg.StripeConnectClientID != "" && cfg.StripeSecretKey != "" && clinicStore != nil {
		redirectURI := cfg.StripeConnectRedirect
		if redirectURI == "" {
			redirectURI = cfg.PublicBaseURL + "/stripe/connect/callback"
		}
		stripeConnectHandler = payments.NewStripeConnectHandler(cfg.StripeConnectClientID, cfg.StripeSecretKey, redirectURI, clinicStore, logger)
		logger.Info("stripe connect handler initialized", "client_id_set", cfg.StripeConnectClientID != "", "redirect_uri", redirectURI)
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
			Store:             msgStore,
			Processed:         processedStore,
			Telnyx:            telnyxClient,
			Conversation:      conversationPublisher,
			Leads:             leadsRepo,
			Logger:            logger,
			Transcript:        smsTranscript,
			ConversationStore: conversationStore,
			ClinicStore:       clinicStore,
			MessagingProfile:  cfg.TelnyxMessagingProfileID,
			StopAck:           cfg.TelnyxStopReply,
			HelpAck:           cfg.TelnyxHelpReply,
			StartAck:          cfg.TelnyxStartReply,
			FirstContactAck:   cfg.TelnyxFirstContactReply,
			VoiceAck:          cfg.TelnyxVoiceAckReply,
			DemoMode:          cfg.DemoMode,
			TrackJobs:         cfg.TelnyxTrackJobs,
			Metrics:           messagingMetrics,
		})
		logger.Info("telnyx webhook handler initialized", "profile_id", cfg.TelnyxMessagingProfileID)
	} else {
		logger.Warn("telnyx webhook handler NOT created - missing prerequisites")
	}

	// Create booking callback handler for browser sidecar outcome notifications
	var bookingCallbackHandler *conversation.BookingCallbackHandler
	if leadsRepo != nil && webhookMessenger != nil {
		bookingCallbackHandler = conversation.NewBookingCallbackHandler(leadsRepo, webhookMessenger, logger)
		logger.Info("booking callback handler initialized")
	}

	// Set up evidence upload S3 (reuse training bucket)
	var evidenceS3 handlers.S3Uploader
	if cfg.S3TrainingBucket != "" {
		if awsCfg, err := mainconfig.LoadAWSConfig(appCtx, cfg); err == nil {
			evidenceS3 = s3.NewFromConfig(awsCfg)
			logger.Info("evidence upload S3 enabled", "bucket", cfg.S3TrainingBucket)
		}
	}

	// GitHub workflow webhook notifications (to Andrew on Telegram)
	var githubWebhookHandler *handlers.GitHubWebhookHandler
	if cfg.GitHubWebhookSecret != "" {
		githubNotifier := handlers.NewTelegramNotifier(cfg.TelegramBotToken, cfg.AndrewTelegramChatID, logger)
		githubWebhookHandler = handlers.NewGitHubWebhookHandler(cfg.GitHubWebhookSecret, githubNotifier, logger)
		logger.Info("github webhook handler initialized")
	} else {
		logger.Warn("github webhook handler not initialized (GITHUB_WEBHOOK_SECRET missing)")
	}

	// Setup router
	routerCfg := &router.Config{
		Logger:                 logger,
		LeadsHandler:           leadsHandler,
		MessagingHandler:       messagingHandler,
		ConversationHandler:    conversationHandler,
		PaymentsHandler:        checkoutHandler,
		FakePayments:           fakePaymentsHandler,
		SquareWebhook:          squareWebhookHandler,
		SquareOAuth:            squareOAuthHandler,
		StripeWebhook:          stripeWebhookHandler,
		StripeConnect:          stripeConnectHandler,
		AdminMessaging:         adminMessagingHandler,
		AdminClinicData:        adminClinicDataHandler,
		TelnyxWebhooks:         telnyxWebhookHandler,
		GitHubWebhook:          githubWebhookHandler,
		ClinicHandler:          clinicHandler,
		ClinicStatsHandler:     clinicStatsHandler,
		ClinicDashboard:        clinicDashboardHandler,
		AdminOnboarding:        adminOnboardingHandler,
		OnboardingToken:        cfg.OnboardingToken,
		ClientRegistration:     clientRegistrationHandler,
		AdminAuthSecret:        cfg.AdminJWTSecret,
		CognitoUserPoolID:      cfg.CognitoUserPoolID,
		CognitoClientID:        cfg.CognitoClientID,
		CognitoRegion:          cfg.CognitoRegion,
		DB:                     sqlDB,
		TranscriptStore:        smsTranscript,
		ClinicStore:            clinicStore,
		KnowledgeRepo:          knowledgeRepo,
		AuditService:           auditSvc,
		MetricsHandler:         metricsHandler,
		CORSAllowedOrigins:     cfg.CORSAllowedOrigins,
		BookingCallbackHandler: bookingCallbackHandler,
		RedisClient:            redisClient,
		HasSMSProvider:         len(cfg.SMSProviderIssues()) == 0,
		PaymentRedirect:        payments.NewRedirectHandler(paymentsRepo, logger),
		AdminBriefs:            newBriefsHandler(dbPool, logger),
		ProspectsHandler:       newProspectsHandler(sqlDB),
		EvidenceS3Client:       evidenceS3,
		EvidenceS3Bucket:       cfg.S3TrainingBucket,
		EvidenceS3Region:       cfg.AWSRegion,
		VoiceAIHandler:         voiceAIHandler,
		StructuredKnowledgeHandler: handlers.NewStructuredKnowledgeHandler(
			conversation.NewStructuredKnowledgeStore(redisClient),
			clinicStore,
			knowledgeRepo,
			logger,
		),
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

	waitForInlineWorker(inlineWorker, logger)

	logger.Info("server stopped")
	fmt.Println("Server exited gracefully")
}

func setupMessagingMetrics() (http.Handler, *observemetrics.MessagingMetrics) {
	registry := prometheus.NewRegistry()
	messagingMetrics := observemetrics.NewMessagingMetrics(registry)
	conversation.RegisterMetrics(registry)
	metricsHandler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	return metricsHandler, messagingMetrics
}

func connectPostgresPool(ctx context.Context, dbURL string, logger *logging.Logger) *pgxpool.Pool {
	if dbURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping postgres", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to postgres")
	return pool
}

func runAutoMigrate(db *sql.DB, logger *logging.Logger) {
	srcDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		logger.Error("auto-migrate: failed to open migrations source", "error", err)
		return
	}
	dbDriver, err := pgmigrate.WithInstance(db, &pgmigrate.Config{})
	if err != nil {
		logger.Error("auto-migrate: failed to create db driver", "error", err)
		return
	}
	m, err := migrate.NewWithInstance("iofs", srcDriver, "postgres", dbDriver)
	if err != nil {
		logger.Error("auto-migrate: failed to create migrator", "error", err)
		return
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		logger.Error("auto-migrate: migration failed", "error", err)
		return
	}
	logger.Info("auto-migrate: database migrations applied")
}

func connectSQLDB(pool *pgxpool.Pool, logger *logging.Logger) *sql.DB {
	if pool == nil {
		return nil
	}
	db := stdlib.OpenDBFromPool(pool)
	if logger != nil {
		logger.Info("sql db wrapper initialized")
	}
	return db
}

func setupConversation(
	ctx context.Context,
	cfg *appconfig.Config,
	dbPool *pgxpool.Pool,
	logger *logging.Logger,
) (*conversation.Publisher, conversation.JobRecorder, conversation.JobUpdater, *conversation.MemoryQueue) {
	var (
		publisher   *conversation.Publisher
		recorder    conversation.JobRecorder
		updater     conversation.JobUpdater
		memoryQueue *conversation.MemoryQueue
	)

	if cfg.UseMemoryQueue {
		if dbPool == nil {
			logger.Error("USE_MEMORY_QUEUE requires DATABASE_URL for job persistence")
			os.Exit(1)
		}
		memoryQueue = conversation.NewMemoryQueue(1024)
		pgStore := conversation.NewPGJobStore(dbPool)
		recorder, updater = pgStore, pgStore
		publisher = conversation.NewPublisher(memoryQueue, pgStore, logger)
		return publisher, recorder, updater, memoryQueue
	}

	awsCfg, err := mainconfig.LoadAWSConfig(ctx, cfg)
	if err != nil {
		logger.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqs.NewFromConfig(awsCfg)
	sqsQueue := conversation.NewSQSQueue(sqsClient, cfg.ConversationQueueURL)
	dynamoClient := dynamodb.NewFromConfig(awsCfg)
	store := conversation.NewJobStore(dynamoClient, cfg.ConversationJobsTable, logger)
	recorder, updater = store, store
	publisher = conversation.NewPublisher(sqsQueue, store, logger)
	return publisher, recorder, updater, memoryQueue
}

func setupInlineWorker(
	ctx context.Context,
	cfg *appconfig.Config,
	logger *logging.Logger,
	messenger conversation.ReplyMessenger,
	messengerReason string,
	jobUpdater conversation.JobUpdater,
	memoryQueue *conversation.MemoryQueue,
	dbPool *pgxpool.Pool,
	sqlDB *sql.DB,
	audit *auditcompliance.AuditService,
	outboxStore *events.OutboxStore,
	resolver payments.OrgNumberResolver,
	optOutChecker conversation.OptOutChecker,
	supervisor conversation.Supervisor,
	redisClient *redis.Client,
	smsTranscript *conversation.SMSTranscriptStore,
) (*conversation.Worker, conversation.Service) {
	if !cfg.UseMemoryQueue || memoryQueue == nil {
		return nil, nil
	}

	leadsRepo := initializeLeadsRepository(dbPool)
	var paymentChecker *payments.Repository
	if dbPool != nil {
		paymentChecker = payments.NewRepository(dbPool, redisClient)
	}
	processor, err := appbootstrap.BuildConversationService(ctx, cfg, leadsRepo, paymentChecker, audit, logger)
	if err != nil {
		logger.Error("failed to configure inline conversation service", "error", err)
		os.Exit(1)
	}

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

	var clinicStore *clinic.Store
	if redisClient != nil {
		clinicStore = clinic.NewStore(redisClient)
	}

	var depositSender conversation.DepositSender
	var depositPreloader *conversation.DepositPreloader
	var convStore *conversation.ConversationStore
	if cfg.PersistConversationHistory {
		convStore = conversation.NewConversationStore(sqlDB)
	}
	if dbPool != nil && outboxStore != nil && paymentChecker != nil {
		numberResolver := resolver
		hasSquareProvider := strings.TrimSpace(cfg.SquareAccessToken) != "" || (cfg.SquareClientID != "" && cfg.SquareClientSecret != "" && cfg.SquareOAuthRedirectURI != "")
		useFakePayments := cfg.AllowFakePayments && !hasSquareProvider
		// Fake payments mode only when Square isn't configured (for testing when sandbox is broken)
		if useFakePayments {
			fakeSvc := payments.NewFakeCheckoutService(cfg.PublicBaseURL, logger)
			depositSender = conversation.NewDepositDispatcher(paymentChecker, fakeSvc, outboxStore, messenger, numberResolver, leadsRepo, smsTranscript, convStore, logger, conversation.WithShortURLs(paymentChecker, cfg.PublicBaseURL))
			logger.Warn("deposit sender initialized in fake payments mode (ALLOW_FAKE_PAYMENTS=true)")
		} else {
			hasStripeProvider := cfg.StripeSecretKey != ""
			if !hasSquareProvider && !hasStripeProvider {
				logger.Warn("deposit sender NOT initialized", "has_db", dbPool != nil, "has_outbox", outboxStore != nil, "has_square_token", cfg.SquareAccessToken != "", "has_stripe_key", cfg.StripeSecretKey != "")
			} else if !hasSquareProvider && hasStripeProvider {
				// Stripe-only mode
				stripeSvc := payments.NewStripeCheckoutService(cfg.StripeSecretKey, cfg.StripeSuccessURL, cfg.StripeCancelURL, logger)
				depositSender = conversation.NewDepositDispatcher(paymentChecker, stripeSvc, outboxStore, messenger, numberResolver, leadsRepo, smsTranscript, convStore, logger, conversation.WithShortURLs(paymentChecker, cfg.PublicBaseURL))
				logger.Info("deposit sender initialized (stripe only)")
			} else {
				usePaymentLinks := payments.UsePaymentLinks(cfg.SquareCheckoutMode, cfg.SquareSandbox)
				squareSvc := payments.NewSquareCheckoutService(cfg.SquareAccessToken, cfg.SquareLocationID, cfg.SquareSuccessURL, cfg.SquareCancelURL, logger).
					WithBaseURL(cfg.SquareBaseURL).
					WithPaymentLinks(usePaymentLinks).
					WithPaymentLinkFallback(cfg.SquareCheckoutAllowFallback)
				if cfg.SquareClientID != "" && cfg.SquareClientSecret != "" && cfg.SquareOAuthRedirectURI != "" {
					oauthSvc := payments.NewSquareOAuthService(
						payments.SquareOAuthConfig{
							ClientID:     cfg.SquareClientID,
							ClientSecret: cfg.SquareClientSecret,
							RedirectURI:  cfg.SquareOAuthRedirectURI,
							Sandbox:      cfg.SquareSandbox,
						},
						dbPool,
						logger,
					)
					squareSvc = squareSvc.WithCredentialsProvider(oauthSvc)
					numberResolver = payments.NewDBOrgNumberResolver(oauthSvc, resolver)
					logger.Info("square oauth wired into inline workers", "sandbox", cfg.SquareSandbox)
				}

				// Use multi-checkout if Stripe is also configured, otherwise just Square
				var checkoutSvc payments.CheckoutProvider = squareSvc
				if cfg.StripeSecretKey != "" && clinicStore != nil {
					stripeSvc := payments.NewStripeCheckoutService(cfg.StripeSecretKey, cfg.StripeSuccessURL, cfg.StripeCancelURL, logger)
					checkoutSvc = payments.NewMultiCheckoutService(squareSvc, stripeSvc, clinicStore, logger)
					logger.Info("multi-checkout service initialized (square + stripe)")
				}

				depositSender = conversation.NewDepositDispatcher(paymentChecker, checkoutSvc, outboxStore, messenger, numberResolver, leadsRepo, smsTranscript, convStore, logger, conversation.WithShortURLs(paymentChecker, cfg.PublicBaseURL))
				depositPreloader = conversation.NewDepositPreloader(squareSvc, 5000, logger) // Default $50 deposit
				logger.Info("deposit sender initialized", "square_location_id", cfg.SquareLocationID, "has_oauth", cfg.SquareClientID != "", "preloader", depositPreloader != nil)
			}
		}
	} else {
		logger.Warn("deposit sender NOT initialized", "has_db", dbPool != nil, "has_outbox", outboxStore != nil, "has_square_token", cfg.SquareAccessToken != "", "has_square_location", cfg.SquareLocationID != "")
	}

	// Initialize notification service for clinic operator alerts
	var notifier conversation.PaymentNotifier
	if clinicStore != nil {
		// Setup email sender (prefer SES over SendGrid)
		var emailSender notify.EmailSender
		if cfg.SESFromEmail != "" {
			// Use AWS SES for email
			sesAwsCfg, err := mainconfig.LoadAWSConfig(ctx, cfg)
			if err != nil {
				logger.Error("failed to load AWS config for SES", "error", err)
			} else {
				sesClient := sesv2.NewFromConfig(sesAwsCfg)
				emailSender = notify.NewSESSender(sesClient, notify.SESConfig{
					FromEmail: cfg.SESFromEmail,
					FromName:  cfg.SESFromName,
				}, logger)
				logger.Info("AWS SES email sender initialized for inline workers", "from", cfg.SESFromEmail)
			}
		}
		if emailSender == nil && cfg.SendGridAPIKey != "" && cfg.SendGridFromEmail != "" {
			emailSender = notify.NewSendGridSender(notify.SendGridConfig{
				APIKey:    cfg.SendGridAPIKey,
				FromEmail: cfg.SendGridFromEmail,
				FromName:  cfg.SendGridFromName,
			}, logger)
			logger.Info("sendgrid email sender initialized for inline workers")
		}
		if emailSender == nil {
			emailSender = notify.NewStubEmailSender(logger)
			logger.Warn("email notifications disabled for inline workers (SES_FROM_EMAIL or SENDGRID_API_KEY not set)")
		}

		// Setup SMS sender for operator notifications
		// Prefer Telnyx from number since Telnyx is the primary SMS provider
		var smsSender notify.SMSSender
		smsFromNumber := cfg.TelnyxFromNumber
		if smsFromNumber == "" {
			smsFromNumber = cfg.TwilioFromNumber
		}
		if messenger != nil && smsFromNumber != "" {
			smsSender = notify.NewSimpleSMSSender(smsFromNumber, func(ctx context.Context, to, from, body string) error {
				return messenger.SendReply(ctx, conversation.OutboundReply{
					To:   to,
					From: from,
					Body: body,
				})
			}, logger)
			logger.Info("sms sender initialized for operator notifications (inline workers)", "from", smsFromNumber)
		} else {
			smsSender = notify.NewStubSMSSender(logger)
			logger.Warn("operator SMS notifications disabled for inline workers (messenger not available or no from number)")
		}

		notifier = notify.NewService(emailSender, smsSender, clinicStore, leadsRepo, logger)
		logger.Info("notification service initialized for inline workers")
	} else {
		logger.Warn("notification service NOT initialized (redis not configured)")
	}

	var processedStore *events.ProcessedStore
	if dbPool != nil {
		processedStore = events.NewProcessedStore(dbPool)
	}

	var msgChecker conversation.ProviderMessageChecker
	if optOutChecker != nil {
		if checker, ok := optOutChecker.(conversation.ProviderMessageChecker); ok {
			msgChecker = checker
		}
	}

	var autoPurger conversation.SandboxAutoPurger
	if cfg.Env != "production" && cfg.SquareSandbox && dbPool != nil {
		phones := clinicdata.ParsePhoneDigitsList(cfg.SandboxAutoPurgePhones)
		if len(phones) > 0 {
			purger := clinicdata.NewPurger(dbPool, redisClient, logger)
			autoPurger = clinicdata.NewSandboxAutoPurger(purger, clinicdata.AutoPurgeConfig{
				Enabled:            true,
				AllowedPhoneDigits: phones,
				Delay:              cfg.SandboxAutoPurgeDelay,
			}, logger)
			logger.Info("sandbox auto purge enabled for inline workers", "delay", cfg.SandboxAutoPurgeDelay.String())
		}
	}

	workerOpts := []conversation.WorkerOption{
		conversation.WithWorkerCount(cfg.WorkerCount),
		conversation.WithDepositSender(depositSender),
		conversation.WithDepositPreloader(depositPreloader),
		conversation.WithPaymentNotifier(notifier),
		conversation.WithSandboxAutoPurger(autoPurger),
		conversation.WithProcessedEventsStore(processedStore),
		conversation.WithOptOutChecker(optOutChecker),
		conversation.WithProviderMessageChecker(msgChecker),
		conversation.WithClinicConfigStore(clinicStore),
		conversation.WithSMSTranscriptStore(smsTranscript),
		conversation.WithConversationStore(convStore),
		conversation.WithSupervisor(supervisor),
		conversation.WithSupervisorMode(conversation.ParseSupervisorMode(cfg.SupervisorMode)),
		conversation.WithWorkerLeadsRepo(leadsRepo),
	}

	// Wire browser sidecar booking client into worker for Moxie booking automation
	if cfg.BrowserSidecarURL != "" {
		browserClient := browser.NewClient(cfg.BrowserSidecarURL, browser.WithLogger(logger))
		workerOpts = append(workerOpts, conversation.WithBrowserBookingClient(browserClient))
		logger.Info("browser booking client wired into inline worker", "url", cfg.BrowserSidecarURL)
	}

	// Wire direct Moxie GraphQL API client (preferred over browser sidecar)
	moxieDryRun := os.Getenv("MOXIE_DRY_RUN") == "true"
	moxieAPIClient := moxieclient.NewClient(logger, moxieclient.WithDryRun(moxieDryRun))
	workerOpts = append(workerOpts, conversation.WithWorkerMoxieClient(moxieAPIClient))
	if moxieDryRun {
		logger.Info("Moxie direct API client wired in DRY RUN mode — no real appointments will be created")
	} else {
		logger.Info("Moxie direct API client wired into inline worker")
	}

	worker := conversation.NewWorker(
		processor,
		memoryQueue,
		jobUpdater,
		messenger,
		bookingBridge,
		logger,
		workerOpts...,
	)
	worker.Start(ctx)
	logger.Info("inline conversation workers started", "count", cfg.WorkerCount)
	return worker, processor
}

func waitForInlineWorker(inlineWorker *conversation.Worker, logger *logging.Logger) {
	if inlineWorker == nil {
		return
	}

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

func setupTelnyxClient(cfg *appconfig.Config, logger *logging.Logger) *telnyxclient.Client {
	if cfg.TelnyxAPIKey == "" {
		logger.Debug("telnyx client not created: API key empty")
		return nil
	}

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
	logger.Debug("telnyx client created successfully")
	return client
}

func newBriefsHandler(pool *pgxpool.Pool, logger *logging.Logger) *handlers.AdminBriefsHandler {
	abs, err := filepath.Abs("research")
	if err != nil {
		abs = ""
	} else if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
		abs = ""
	}
	h := handlers.NewAdminBriefsHandler(abs, logger)
	if pool != nil {
		h.SetRepository(briefs.NewPostgresBriefsRepository(pool))
	}
	return h
}

func newProspectsHandler(sqlDB *sql.DB) *prospects.Handler {
	h := prospects.NewHandler(prospects.NewRepository(sqlDB))
	// Look for research/ dir relative to working directory
	if abs, err := filepath.Abs("research"); err == nil {
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			h.SetResearchDir(abs)
		}
	}
	return h
}

func initializeLeadsRepository(dbPool *pgxpool.Pool) leads.Repository {
	if dbPool != nil {
		return leads.NewPostgresRepository(dbPool)
	}
	return leads.NewInMemoryRepository()
}

// trigger deploy for task def revision 387
