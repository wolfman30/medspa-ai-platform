package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/wolfman30/medspa-ai-platform/internal/api/router"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/bootstrap"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
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

	metricsHandler, messagingMetrics := bootstrap.SetupMessagingMetrics()

	// Set up signal-aware context
	appCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db := bootstrap.BootstrapDB(appCtx, cfg, logger)
	if db.Pool != nil {
		defer db.Pool.Close()
	}
	if db.SQLDB != nil {
		defer db.SQLDB.Close()
	}
	dbPool := db.Pool
	sqlDB := db.SQLDB
	conversationStore := db.ConversationStore
	auditSvc := db.AuditSvc
	leadsRepo := db.LeadsRepo
	msgStore := db.MsgStore
	conversationPublisher, jobRecorder, jobUpdater, memoryQueue := bootstrap.SetupConversation(bootstrap.ConversationSetupDeps{Ctx: appCtx, Cfg: cfg, DBPool: dbPool, Logger: logger})

	// Clinic bootstrap (redis + clinic config stores)
	clinicBoot := bootstrap.BootstrapClinic(cfg, appCtx, logger)
	redisClient := clinicBoot.RedisClient
	clinicStore := clinicBoot.ClinicStore
	smsTranscript := clinicBoot.SMSTranscript

	// Initialize handlers
	leadsHandler := leads.NewHandler(leadsRepo, logger)
	messagingBoot := bootstrap.BootstrapMessaging(bootstrap.MessagingDeps{
		Cfg: cfg, Logger: logger, ConversationPublisher: conversationPublisher, LeadsRepo: leadsRepo,
		MessageStore: msgStore, AuditService: auditSvc, ConversationStore: conversationStore,
		SMSTranscriptStore: smsTranscript, ClinicStore: clinicStore,
	})
	resolver := messagingBoot.Resolver
	webhookMessenger := messagingBoot.WebhookMessenger
	webhookMessengerReason := messagingBoot.MessengerReason
	messagingHandler := messagingBoot.MessagingHandler

	telnyxClient := bootstrap.SetupTelnyxClient(cfg, logger)
	adminMessagingHandler := bootstrap.BuildAdminMessagingHandler(bootstrap.AdminMessagingDeps{
		Cfg: cfg, Logger: logger, MessageStore: msgStore, TelnyxClient: telnyxClient, MessagingMetrics: messagingMetrics,
	})
	var paymentsRepo *payments.Repository
	var outboxStore *events.OutboxStore
	var processedStore *events.ProcessedStore
	if dbPool != nil {
		paymentsRepo = payments.NewRepository(dbPool, redisClient)
		outboxStore = events.NewOutboxStore(dbPool)
		processedStore = events.NewProcessedStore(dbPool)
	}

	clinicHandler, clinicStatsHandler, clinicDashboardHandler := bootstrap.BuildClinicHandlers(logger, clinicStore, dbPool)

	adminClinicDataHandler := bootstrap.BuildAdminClinicDataHandler(bootstrap.AdminClinicDataDeps{
		AppCtx: appCtx, Cfg: cfg, Logger: logger, DBPool: dbPool, RedisClient: redisClient,
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

	inlineWorker, conversationService := bootstrap.SetupInlineWorker(bootstrap.InlineWorkerDeps{
		Ctx:           appCtx,
		Cfg:           cfg,
		Logger:        logger,
		Messenger:     webhookMessenger,
		MessengerNote: webhookMessengerReason,
		JobUpdater:    jobUpdater,
		MemoryQueue:   memoryQueue,
		DBPool:        dbPool,
		SQLDB:         sqlDB,
		Audit:         auditSvc,
		OutboxStore:   outboxStore,
		Resolver:      resolver,
		OptOutChecker: msgStore,
		Supervisor:    supervisor,
		RedisClient:   redisClient,
		SMSTranscript: smsTranscript,
	})
	if conversationService != nil {
		conversationHandler.SetService(conversationService)
	}

	voiceBoot := bootstrap.BootstrapVoice(bootstrap.VoiceDeps{
		Cfg:                   cfg,
		Logger:                logger,
		MsgStore:              msgStore,
		ClinicStore:           clinicStore,
		ConversationPublisher: conversationPublisher,
		ConversationService:   conversationService,
		ConversationStore:     conversationStore,
		RedisClient:           redisClient,
		WebhookMessenger:      webhookMessenger,
		LeadsRepo:             leadsRepo,
		Resolver:              resolver,
	})
	voiceAIHandler := voiceBoot.VoiceAIHandler
	voiceWSHandler := voiceBoot.VoiceWSHandler
	callControlHandler := voiceBoot.CallControl

	paymentBoot := bootstrap.BootstrapPayments(bootstrap.PaymentsDeps{
		AppCtx:                appCtx,
		Cfg:                   cfg,
		Logger:                logger,
		DBPool:                dbPool,
		LeadsRepo:             leadsRepo,
		RedisClient:           redisClient,
		OutboxStore:           outboxStore,
		ProcessedStore:        processedStore,
		Resolver:              resolver,
		PaymentsRepo:          paymentsRepo,
		ClinicStore:           clinicStore,
		ConversationPublisher: conversationPublisher,
	})
	checkoutHandler := paymentBoot.CheckoutHandler
	squareWebhookHandler := paymentBoot.SquareWebhookHandler
	squareOAuthHandler := paymentBoot.SquareOAuthHandler
	fakePaymentsHandler := paymentBoot.FakePaymentsHandler
	stripeWebhookHandler := paymentBoot.StripeWebhookHandler
	stripeConnectHandler := paymentBoot.StripeConnectHandler

	telnyxWebhookHandler := bootstrap.BuildTelnyxWebhookHandler(bootstrap.TelnyxWebhookDeps{
		Cfg: cfg, Logger: logger, MsgStore: msgStore, TelnyxClient: telnyxClient,
		ProcessedStore: processedStore, ConversationPub: conversationPublisher,
		LeadsRepo: leadsRepo, SMSTranscript: smsTranscript,
		ConversationStore: conversationStore, ClinicStore: clinicStore,
		MessagingMetrics: messagingMetrics,
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

	evidenceS3 := bootstrap.BuildEvidenceS3(appCtx, cfg, logger)

	// Notifications bootstrap
	githubWebhookHandler := bootstrap.BootstrapNotifications(cfg, logger)

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
		AdminBriefs:            bootstrap.NewBriefsHandler(dbPool, logger),
		ProspectsHandler:       bootstrap.NewProspectsHandler(sqlDB),
		StoriesHandler:         bootstrap.NewStoriesHandler(sqlDB),
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
	runServer(appCtx, r, cfg.Port, logger, inlineWorker)
}
