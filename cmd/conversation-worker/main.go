package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/bookings"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
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

	processor, err := appbootstrap.BuildConversationService(ctx, cfg, leadsRepo, paymentChecker, logger)
	if err != nil {
		logger.Error("failed to configure conversation service", "error", err)
		os.Exit(1)
	}
	var (
		messenger         conversation.ReplyMessenger
		messengerProvider string
		messengerReason   string
		depositSender     conversation.DepositSender
	)
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
			depositSender = conversation.NewDepositDispatcher(paymentChecker, squareSvc, outbox, messenger, numberResolver, logger)
			logger.Info("deposit sender initialized for async workers", "has_oauth", oauthSvc != nil, "square_location_id", cfg.SquareLocationID)
		} else {
			logger.Warn("deposit sender NOT initialized for async workers", "has_square_token", cfg.SquareAccessToken != "", "has_oauth", oauthSvc != nil)
		}
	}

	// Initialize notification service for clinic operator alerts
	var notifier conversation.PaymentNotifier
	if cfg.RedisAddr != "" {
		redisClient := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
		})
		if err := redisClient.Ping(ctx).Err(); err != nil {
			logger.Warn("redis not available, clinic notifications disabled", "error", err)
		} else {
			clinicStore := clinic.NewStore(redisClient)

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
	}

	var processedStore *events.ProcessedStore
	if dbPool != nil {
		processedStore = events.NewProcessedStore(dbPool)
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
		conversation.WithProcessedEventsStore(processedStore),
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
