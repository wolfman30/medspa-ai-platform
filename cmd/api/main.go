package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	"github.com/wolfman30/medspa-ai-platform/internal/api/router"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/archive"
	"github.com/wolfman30/medspa-ai-platform/internal/briefs"
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
	observemetrics "github.com/wolfman30/medspa-ai-platform/internal/observability/metrics"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/internal/prospects"
	"github.com/wolfman30/medspa-ai-platform/internal/stories"
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

	// Set up evidence upload S3 (reuse training bucket)
	var evidenceS3 handlers.S3Uploader
	if cfg.S3TrainingBucket != "" {
		if awsCfg, err := mainconfig.LoadAWSConfig(appCtx, cfg); err == nil {
			evidenceS3 = s3.NewFromConfig(awsCfg)
			logger.Info("evidence upload S3 enabled", "bucket", cfg.S3TrainingBucket)
		}
	}

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

// setupInlineWorker moved to bootstrap_worker.go

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

func newStoriesHandler(sqlDB *sql.DB) *stories.Handler {
	return stories.NewHandler(stories.NewRepository(sqlDB))
}

func initializeLeadsRepository(dbPool *pgxpool.Pool) leads.Repository {
	if dbPool != nil {
		return leads.NewPostgresRepository(dbPool)
	}
	return leads.NewInMemoryRepository()
}

// trigger deploy for task def revision 387
