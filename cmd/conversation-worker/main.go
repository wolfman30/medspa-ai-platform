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
	"github.com/wolfman30/medspa-ai-platform/cmd/mainconfig"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/bookings"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
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
	if dbPool != nil {
		leadsRepo = leads.NewPostgresRepository(dbPool)
	}

	processor, err := appbootstrap.BuildConversationService(ctx, cfg, leadsRepo, logger)
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
	numberResolver := messaging.NewStaticOrgResolver(orgRouting)

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
			payRepo := payments.NewRepository(dbPool)
			outbox := events.NewOutboxStore(dbPool)
			depositSender = conversation.NewDepositDispatcher(payRepo, squareSvc, outbox, messenger, numberResolver, logger)
			logger.Info("deposit sender initialized for async workers", "has_oauth", oauthSvc != nil, "square_location_id", cfg.SquareLocationID)
		} else {
			logger.Warn("deposit sender NOT initialized for async workers", "has_square_token", cfg.SquareAccessToken != "", "has_oauth", oauthSvc != nil)
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
