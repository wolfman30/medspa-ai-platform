package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/bookings"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/clinicdata"
	auditcompliance "github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/notify"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

func main() {
	cfg := appconfig.Load()
	logger := logging.New(cfg.LogLevel)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if cfg.UseMemoryQueue {
		logger.Error("conversation worker cannot run when USE_MEMORY_QUEUE=true; run inline workers via the API process instead")
		os.Exit(1)
	}

	var dbPool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("worker failed to connect to postgres", "error", err)
			os.Exit(1)
		}
		dbPool = pool
		defer dbPool.Close()
	}
	var sqlDB *sql.DB
	if dbPool != nil {
		sqlDB = stdlib.OpenDBFromPool(dbPool)
		defer sqlDB.Close()
	}
	var auditSvc *auditcompliance.AuditService
	if sqlDB != nil {
		auditSvc = auditcompliance.NewAuditService(sqlDB)
	}

	awsConfig, err := mainconfig.LoadAWSConfig(context.Background(), cfg)
	if err != nil {
		logger.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}

	sqsClient := sqs.NewFromConfig(awsConfig)
	queue := conversation.NewSQSQueue(sqsClient, cfg.ConversationQueueURL)
	dynamoClient := dynamodb.NewFromConfig(awsConfig)
	jobStore := conversation.NewJobStore(dynamoClient, cfg.ConversationJobsTable, logger)

	var leadsRepo leads.Repository
	var paymentChecker *payments.Repository
	if dbPool != nil {
		leadsRepo = leads.NewPostgresRepository(dbPool)
		paymentChecker = payments.NewRepository(dbPool)
	}
	msgStore := messaging.NewStore(dbPool)

	processor, err := appbootstrap.BuildConversationService(ctx, cfg, leadsRepo, paymentChecker, auditSvc, logger)
	if err != nil {
		logger.Error("failed to configure conversation service", "error", err)
		os.Exit(1)
	}
	supervisor, err := appbootstrap.BuildSupervisor(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to configure supervisor", "error", err)
		os.Exit(1)
	}
	var (
		messenger         conversation.ReplyMessenger
		messengerProvider string
		messengerReason   string
		depositSender     conversation.DepositSender
	)
	var convStore *conversation.ConversationStore
	if cfg.PersistConversationHistory {
		convStore = conversation.NewConversationStore(sqlDB)
	}
	var redisClient *redis.Client
	if cfg.RedisAddr != "" {
		redisOptions := &redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		}
		if cfg.RedisTLS {
			redisOptions.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		redisClient = redis.NewClient(redisOptions)
		if err := redisClient.Ping(ctx).Err(); err != nil {
			logger.Warn("redis not available", "error", err)
			redisClient = nil
		}
	}
	smsTranscript := conversation.NewSMSTranscriptStore(redisClient)
	var clinicStore *clinic.Store
	if redisClient != nil {
		clinicStore = clinic.NewStore(redisClient)
	}
	messengerCfg := messaging.ProviderSelectionConfig{
		Preference:       cfg.SMSProvider,
		TelnyxAPIKey:     cfg.TelnyxAPIKey,
		TelnyxProfileID:  cfg.TelnyxMessagingProfileID,
		TwilioAccountSID: cfg.TwilioAccountSID,
		TwilioAuthToken:  cfg.TwilioAuthToken,
		TwilioFromNumber: cfg.TwilioFromNumber,
	}
	messenger, messengerProvider, messengerReason = messaging.BuildReplyMessenger(messengerCfg, logger)
	if messenger != nil {
		logger.Info("sms messenger initialized for async workers",
			"provider", messengerProvider,
			"preference", cfg.SMSProvider,
		)
		messenger = messaging.WrapWithDemoMode(messenger, messaging.DemoModeConfig{
			Enabled: cfg.DemoMode,
			Prefix:  cfg.DemoModePrefix,
			Suffix:  cfg.DemoModeSuffix,
			Logger:  logger,
		})
		messenger = messaging.WrapWithDisclaimers(messenger, messaging.DisclaimerWrapperConfig{
			Enabled:           cfg.DisclaimerEnabled,
			Level:             cfg.DisclaimerLevel,
			FirstMessageOnly:  cfg.DisclaimerFirstOnly,
			Logger:            logger,
			Audit:             auditSvc,
			ConversationStore: convStore,
			TranscriptStore:   smsTranscript,
		})
	} else {
		logger.Warn("sms replies disabled for async workers",
			"preference", cfg.SMSProvider,
			"reason", messengerReason,
		)
	}

	orgRouting := map[string]string{}
	if raw := strings.TrimSpace(cfg.TwilioOrgMapJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &orgRouting); err != nil {
			logger.Warn("failed to parse TWILIO_ORG_MAP_JSON", "error", err)
		}
	}
	var numberResolver payments.OrgNumberResolver = messaging.NewStaticOrgResolver(orgRouting)

	var bookingBridge conversation.BookingServiceAdapter
	var oauthSvc *payments.SquareOAuthService
	if dbPool != nil {
		repo := bookings.NewRepository(dbPool)
		bookingBridge = conversation.BookingServiceAdapter{
			Service: bookings.NewService(repo, logger),
		}

		var squareSvc *payments.SquareCheckoutService
		if cfg.SquareAccessToken != "" || (cfg.SquareClientID != "" && cfg.SquareClientSecret != "" && cfg.SquareOAuthRedirectURI != "") {
			squareSvc = payments.NewSquareCheckoutService(cfg.SquareAccessToken, cfg.SquareLocationID, cfg.SquareSuccessURL, cfg.SquareCancelURL, logger).WithBaseURL(cfg.SquareBaseURL)
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
			squareSvc = squareSvc.WithCredentialsProvider(oauthSvc)
			numberResolver = payments.NewDBOrgNumberResolver(oauthSvc, numberResolver)
			refreshWorker := payments.NewTokenRefreshWorker(oauthSvc, logger)
			go refreshWorker.Start(ctx)
		}
		if squareSvc != nil {
			outbox := events.NewOutboxStore(dbPool)
			depositSender = conversation.NewDepositDispatcher(paymentChecker, squareSvc, outbox, messenger, numberResolver, leadsRepo, smsTranscript, convStore, logger)
			logger.Info("deposit sender initialized for async workers", "has_oauth", oauthSvc != nil, "square_location_id", cfg.SquareLocationID)
		} else {
			logger.Warn("deposit sender NOT initialized for async workers", "has_square_token", cfg.SquareAccessToken != "", "has_oauth", oauthSvc != nil)
		}
	}

	// Initialize notification service for clinic operator alerts
	var notifier conversation.PaymentNotifier
	if clinicStore != nil {
		// Setup email sender
		var emailSender notify.EmailSender
		if cfg.SendGridAPIKey != "" && cfg.SendGridFromEmail != "" {
			emailSender = notify.NewSendGridSender(notify.SendGridConfig{
				APIKey:    cfg.SendGridAPIKey,
				FromEmail: cfg.SendGridFromEmail,
				FromName:  cfg.SendGridFromName,
			}, logger)
			logger.Info("sendgrid email sender initialized for notifications")
		} else {
			emailSender = notify.NewStubEmailSender(logger)
			logger.Warn("email notifications disabled (SENDGRID_API_KEY or SENDGRID_FROM_EMAIL not set)")
		}

		// Setup SMS sender for operator notifications (reuse existing messenger)
		var smsSender notify.SMSSender
		if messenger != nil && cfg.TwilioFromNumber != "" {
			smsSender = notify.NewSimpleSMSSender(cfg.TwilioFromNumber, func(ctx context.Context, to, from, body string) error {
				return messenger.SendReply(ctx, conversation.OutboundReply{
					To:   to,
					From: from,
					Body: body,
				})
			}, logger)
			logger.Info("sms sender initialized for operator notifications")
		} else {
			smsSender = notify.NewStubSMSSender(logger)
			logger.Warn("operator SMS notifications disabled (messenger not available)")
		}

		notifier = notify.NewService(emailSender, smsSender, clinicStore, leadsRepo, logger)
		logger.Info("notification service initialized for clinic operator alerts")
	}

	var processedStore *events.ProcessedStore
	if dbPool != nil {
		processedStore = events.NewProcessedStore(dbPool)
	}

	var autoPurger conversation.SandboxAutoPurger
	if cfg.Env != "production" && cfg.SquareSandbox && dbPool != nil {
		phones := clinicdata.ParsePhoneDigitsList(cfg.SandboxAutoPurgePhones)
		if len(phones) > 0 {
			if redisClient == nil {
				logger.Warn("sandbox auto purge disabled: redis not configured")
			} else {
				purger := clinicdata.NewPurger(dbPool, redisClient, logger)
				autoPurger = clinicdata.NewSandboxAutoPurger(purger, clinicdata.AutoPurgeConfig{
					Enabled:            true,
					AllowedPhoneDigits: phones,
					Delay:              cfg.SandboxAutoPurgeDelay,
				}, logger)
				logger.Info("sandbox auto purge enabled", "delay", cfg.SandboxAutoPurgeDelay.String())
			}
		}
	}

	worker := conversation.NewWorker(
		processor,
		queue,
		jobStore,
		messenger,
		bookingBridge,
		logger,
		conversation.WithWorkerCount(cfg.WorkerCount),
		conversation.WithDepositSender(depositSender),
		conversation.WithPaymentNotifier(notifier),
		conversation.WithSandboxAutoPurger(autoPurger),
		conversation.WithProcessedEventsStore(processedStore),
		conversation.WithOptOutChecker(msgStore),
		conversation.WithClinicConfigStore(clinicStore),
		conversation.WithSMSTranscriptStore(smsTranscript),
		conversation.WithConversationStore(convStore),
		conversation.WithSupervisor(supervisor),
		conversation.WithSupervisorMode(conversation.ParseSupervisorMode(cfg.SupervisorMode)),
	)

	worker.Start(ctx)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down conversation worker...")
	cancel()

	doneCtx, doneCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer doneCancel()

	waitCh := make(chan struct{})
	go func() {
		worker.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		logger.Info("conversation worker stopped")
	case <-doneCtx.Done():
		logger.Error("conversation worker shutdown timed out", "error", doneCtx.Err())
	}
}
