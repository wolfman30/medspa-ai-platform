package main

import (
	"context"
	"os"

	"github.com/joho/godotenv"
	"github.com/wolfman30/medspa-ai-platform/internal/api/router"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	auditcompliance "github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
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

	// Clinic bootstrap (redis + clinic config stores)
	clinicBoot := bootstrapClinic(cfg, appCtx, logger)
	redisClient := clinicBoot.redisClient
	clinicStore := clinicBoot.clinicStore
	smsTranscript := clinicBoot.smsTranscript

	// Initialize handlers
	leadsHandler := leads.NewHandler(leadsRepo, logger)
	messagingBoot := bootstrapMessaging(cfg, logger, conversationPublisher, leadsRepo, msgStore, auditSvc, conversationStore, smsTranscript, clinicStore)
	resolver := messagingBoot.resolver
	webhookMessenger := messagingBoot.webhookMessenger
	webhookMessengerReason := messagingBoot.messengerReason
	messagingHandler := messagingBoot.messagingHandler

	telnyxClient := setupTelnyxClient(cfg, logger)
	adminMessagingHandler := buildAdminMessagingHandler(cfg, logger, msgStore, telnyxClient, messagingMetrics)
	var paymentsRepo *payments.Repository
	var outboxStore *events.OutboxStore
	var processedStore *events.ProcessedStore
	if dbPool != nil {
		paymentsRepo = payments.NewRepository(dbPool, redisClient)
		outboxStore = events.NewOutboxStore(dbPool)
		processedStore = events.NewProcessedStore(dbPool)
	}

	clinicHandler, clinicStatsHandler, clinicDashboardHandler := buildClinicHandlers(logger, clinicStore, dbPool)

	adminClinicDataHandler := buildAdminClinicDataHandler(adminClinicDataDeps{
		appCtx: appCtx, cfg: cfg, logger: logger, dbPool: dbPool, redisClient: redisClient,
	})

	var adminOnboardingHandler *handlers.AdminOnboardingHandler
	if clinicStore != nil {
		adminOnboardingHandler = handlers.NewAdminOnboardingHandler(handlers.AdminOnboardingConfig{
			DB: dbPool, Redis: redisClient, ClinicStore: clinicStore, Logger: logger,
		})
	}

	var clientRegistrationHandler *handlers.ClientRegistrationHandler
	if sqlDB != nil {
		clientRegistrationHandler = handlers.NewClientRegistrationHandler(sqlDB, redisClient, logger)
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

	inlineWorker, conversationService := setupInlineWorker(inlineWorkerDeps{
		ctx:           appCtx,
		cfg:           cfg,
		logger:        logger,
		messenger:     webhookMessenger,
		messengerNote: webhookMessengerReason,
		jobUpdater:    jobUpdater,
		memoryQueue:   memoryQueue,
		dbPool:        dbPool,
		sqlDB:         sqlDB,
		audit:         auditSvc,
		outboxStore:   outboxStore,
		resolver:      resolver,
		optOutChecker: msgStore,
		supervisor:    supervisor,
		redisClient:   redisClient,
		smsTranscript: smsTranscript,
	})
	if conversationService != nil {
		conversationHandler.SetService(conversationService)
	}

	voiceBoot := bootstrapVoice(voiceDeps{
		cfg:                   cfg,
		logger:                logger,
		msgStore:              msgStore,
		clinicStore:           clinicStore,
		conversationPublisher: conversationPublisher,
		conversationService:   conversationService,
		conversationStore:     conversationStore,
		redisClient:           redisClient,
		webhookMessenger:      webhookMessenger,
		leadsRepo:             leadsRepo,
		resolver:              resolver,
	})
	voiceAIHandler := voiceBoot.voiceAIHandler
	voiceWSHandler := voiceBoot.voiceWSHandler
	callControlHandler := voiceBoot.callControl

	paymentBoot := bootstrapPayments(paymentsDeps{
		appCtx:                appCtx,
		cfg:                   cfg,
		logger:                logger,
		dbPool:                dbPool,
		leadsRepo:             leadsRepo,
		redisClient:           redisClient,
		outboxStore:           outboxStore,
		processedStore:        processedStore,
		resolver:              resolver,
		paymentsRepo:          paymentsRepo,
		clinicStore:           clinicStore,
		conversationPublisher: conversationPublisher,
	})
	checkoutHandler := paymentBoot.checkoutHandler
	squareWebhookHandler := paymentBoot.squareWebhookHandler
	squareOAuthHandler := paymentBoot.squareOAuthHandler
	fakePaymentsHandler := paymentBoot.fakePaymentsHandler
	stripeWebhookHandler := paymentBoot.stripeWebhookHandler
	stripeConnectHandler := paymentBoot.stripeConnectHandler

	telnyxWebhookHandler := buildTelnyxWebhookHandler(twDeps{
		cfg: cfg, logger: logger, msgStore: msgStore, telnyxClient: telnyxClient,
		processedStore: processedStore, conversationPub: conversationPublisher,
		leadsRepo: leadsRepo, smsTranscript: smsTranscript,
		conversationStore: conversationStore, clinicStore: clinicStore,
		messagingMetrics: messagingMetrics,
	})

	// Wire missed-call text-back into call control handler
	if callControlHandler != nil && telnyxWebhookHandler != nil {
		callControlHandler.SetMissedCallTexter(telnyxWebhookHandler)
		logger.Info("call control handler wired with missed-call text-back")
	}

	// Create booking callback handler for outcome notifications
	var bookingCallbackHandler *conversation.BookingCallbackHandler
	if leadsRepo != nil && webhookMessenger != nil {
		bookingCallbackHandler = conversation.NewBookingCallbackHandler(leadsRepo, webhookMessenger, logger)
		logger.Info("booking callback handler initialized")
	}

	evidenceS3 := buildEvidenceS3(appCtx, cfg, logger)

	// Notifications bootstrap
	githubWebhookHandler := bootstrapNotifications(cfg, logger)

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
		StoriesHandler:         newStoriesHandler(sqlDB),
		EvidenceS3Client:       evidenceS3,
		EvidenceS3Bucket:       cfg.S3TrainingBucket,
		EvidenceS3Region:       cfg.AWSRegion,
		VoiceAIHandler:         voiceAIHandler,
		VoiceWSHandler:         voiceWSHandler,
		CallControlHandler:     callControlHandler,
		StructuredKnowledgeHandler: handlers.NewStructuredKnowledgeHandler(
			conversation.NewStructuredKnowledgeStore(redisClient),
			clinicStore,
			knowledgeRepo,
			logger,
		),
	}
	r := router.New(routerCfg)
	runServer(r, cfg.Port, logger, inlineWorker, stop)
}
