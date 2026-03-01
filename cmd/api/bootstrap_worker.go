package main

import (
	"context"
	"database/sql"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/jackc/pgx/v5/pgxpool"
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
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/notify"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// depositDeps holds everything needed to construct a deposit sender.
type depositDeps struct {
	cfg            *appconfig.Config
	logger         *logging.Logger
	dbPool         *pgxpool.Pool
	outboxStore    *events.OutboxStore
	paymentChecker *payments.Repository
	messenger      conversation.ReplyMessenger
	resolver       payments.OrgNumberResolver
	leadsRepo      leads.Repository
	smsTranscript  *conversation.SMSTranscriptStore
	convStore      *conversation.ConversationStore
	clinicStore    *clinic.Store
}

// depositResult holds the outputs of buildDepositSender.
type depositResult struct {
	sender    conversation.DepositSender
	preloader *conversation.DepositPreloader
}

// buildDepositSender selects the correct payment provider (Fake, Stripe-only,
// Square, or Multi) and wires the deposit dispatcher.
func buildDepositSender(deps depositDeps) depositResult {
	cfg := deps.cfg
	logger := deps.logger

	if deps.dbPool == nil || deps.outboxStore == nil || deps.paymentChecker == nil {
		logger.Warn("deposit sender NOT initialized — missing prerequisites")
		return depositResult{}
	}

	numberResolver := deps.resolver
	hasSquare := strings.TrimSpace(cfg.SquareAccessToken) != "" ||
		(cfg.SquareClientID != "" && cfg.SquareClientSecret != "" && cfg.SquareOAuthRedirectURI != "")
	hasStripe := cfg.StripeSecretKey != ""

	// Fake payments mode: only when Square isn't configured
	if cfg.AllowFakePayments && !hasSquare {
		fakeSvc := payments.NewFakeCheckoutService(cfg.PublicBaseURL, logger)
		logger.Warn("deposit sender initialized in fake payments mode")
		return depositResult{
			sender: conversation.NewDepositDispatcher(deps.paymentChecker, fakeSvc, deps.outboxStore, deps.messenger, numberResolver, deps.leadsRepo, deps.smsTranscript, deps.convStore, logger, conversation.WithShortURLs(deps.paymentChecker, cfg.PublicBaseURL)),
		}
	}

	// Stripe-only mode
	if !hasSquare && hasStripe {
		stripeSvc := payments.NewStripeCheckoutService(cfg.StripeSecretKey, cfg.StripeSuccessURL, cfg.StripeCancelURL, logger)
		logger.Info("deposit sender initialized (stripe only)")
		return depositResult{
			sender: conversation.NewDepositDispatcher(deps.paymentChecker, stripeSvc, deps.outboxStore, deps.messenger, numberResolver, deps.leadsRepo, deps.smsTranscript, deps.convStore, logger, conversation.WithShortURLs(deps.paymentChecker, cfg.PublicBaseURL)),
		}
	}

	// No provider at all
	if !hasSquare && !hasStripe {
		logger.Warn("deposit sender NOT initialized — no payment provider configured")
		return depositResult{}
	}

	// Square (possibly with Stripe multi-checkout)
	return buildSquareDepositSender(deps, numberResolver)
}

// buildSquareDepositSender handles the Square + optional Stripe multi-checkout path.
func buildSquareDepositSender(deps depositDeps, numberResolver payments.OrgNumberResolver) depositResult {
	cfg := deps.cfg
	logger := deps.logger

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
			deps.dbPool,
			logger,
		)
		squareSvc = squareSvc.WithCredentialsProvider(oauthSvc)
		numberResolver = payments.NewDBOrgNumberResolver(oauthSvc, numberResolver)
		logger.Info("square oauth wired into inline workers", "sandbox", cfg.SquareSandbox)
	}

	var checkoutSvc payments.CheckoutProvider = squareSvc
	if cfg.StripeSecretKey != "" && deps.clinicStore != nil {
		stripeSvc := payments.NewStripeCheckoutService(cfg.StripeSecretKey, cfg.StripeSuccessURL, cfg.StripeCancelURL, logger)
		checkoutSvc = payments.NewMultiCheckoutService(squareSvc, stripeSvc, deps.clinicStore, logger)
		logger.Info("multi-checkout service initialized (square + stripe)")
	}

	preloader := conversation.NewDepositPreloader(squareSvc, 5000, logger)
	logger.Info("deposit sender initialized", "square_location_id", cfg.SquareLocationID)

	return depositResult{
		sender:    conversation.NewDepositDispatcher(deps.paymentChecker, checkoutSvc, deps.outboxStore, deps.messenger, numberResolver, deps.leadsRepo, deps.smsTranscript, deps.convStore, logger, conversation.WithShortURLs(deps.paymentChecker, cfg.PublicBaseURL)),
		preloader: preloader,
	}
}

// buildNotificationService constructs the email + SMS notification pipeline.
func buildNotificationService(
	ctx context.Context,
	cfg *appconfig.Config,
	logger *logging.Logger,
	clinicStore *clinic.Store,
	leadsRepo leads.Repository,
	messenger conversation.ReplyMessenger,
) conversation.PaymentNotifier {
	if clinicStore == nil {
		logger.Warn("notification service NOT initialized (redis not configured)")
		return nil
	}

	emailSender := buildEmailSender(ctx, cfg, logger)
	smsSender := buildSMSSender(cfg, logger, messenger)

	notifier := notify.NewService(emailSender, smsSender, clinicStore, leadsRepo, logger)
	logger.Info("notification service initialized for inline workers")
	return notifier
}

// buildEmailSender picks SES → SendGrid → Stub in priority order.
func buildEmailSender(ctx context.Context, cfg *appconfig.Config, logger *logging.Logger) notify.EmailSender {
	if cfg.SESFromEmail != "" {
		sesAwsCfg, err := mainconfig.LoadAWSConfig(ctx, cfg)
		if err != nil {
			logger.Error("failed to load AWS config for SES", "error", err)
		} else {
			sesClient := sesv2.NewFromConfig(sesAwsCfg)
			logger.Info("AWS SES email sender initialized", "from", cfg.SESFromEmail)
			return notify.NewSESSender(sesClient, notify.SESConfig{
				FromEmail: cfg.SESFromEmail,
				FromName:  cfg.SESFromName,
			}, logger)
		}
	}

	if cfg.SendGridAPIKey != "" && cfg.SendGridFromEmail != "" {
		logger.Info("sendgrid email sender initialized")
		return notify.NewSendGridSender(notify.SendGridConfig{
			APIKey:    cfg.SendGridAPIKey,
			FromEmail: cfg.SendGridFromEmail,
			FromName:  cfg.SendGridFromName,
		}, logger)
	}

	logger.Warn("email notifications disabled (SES_FROM_EMAIL or SENDGRID_API_KEY not set)")
	return notify.NewStubEmailSender(logger)
}

// buildSMSSender creates an operator SMS sender using the outbound messenger.
func buildSMSSender(cfg *appconfig.Config, logger *logging.Logger, messenger conversation.ReplyMessenger) notify.SMSSender {
	smsFromNumber := cfg.TelnyxFromNumber
	if smsFromNumber == "" {
		smsFromNumber = cfg.TwilioFromNumber
	}

	if messenger != nil && smsFromNumber != "" {
		logger.Info("sms sender initialized for operator notifications", "from", smsFromNumber)
		return notify.NewSimpleSMSSender(smsFromNumber, func(ctx context.Context, to, from, body string) error {
			return messenger.SendReply(ctx, conversation.OutboundReply{
				To:   to,
				From: from,
				Body: body,
			})
		}, logger)
	}

	logger.Warn("operator SMS notifications disabled (messenger not available or no from number)")
	return notify.NewStubSMSSender(logger)
}

// buildAutoPurger creates a sandbox auto-purger if configured.
func buildAutoPurger(cfg *appconfig.Config, logger *logging.Logger, dbPool *pgxpool.Pool, redisClient *redis.Client) conversation.SandboxAutoPurger {
	if cfg.Env == "production" || !cfg.SquareSandbox || dbPool == nil {
		return nil
	}

	phones := clinicdata.ParsePhoneDigitsList(cfg.SandboxAutoPurgePhones)
	if len(phones) == 0 {
		return nil
	}

	purger := clinicdata.NewPurger(dbPool, redisClient, logger)
	logger.Info("sandbox auto purge enabled", "delay", cfg.SandboxAutoPurgeDelay.String())
	return clinicdata.NewSandboxAutoPurger(purger, clinicdata.AutoPurgeConfig{
		Enabled:            true,
		AllowedPhoneDigits: phones,
		Delay:              cfg.SandboxAutoPurgeDelay,
	}, logger)
}

// inlineWorkerDeps holds everything setupInlineWorker needs.
type inlineWorkerDeps struct {
	ctx           context.Context
	cfg           *appconfig.Config
	logger        *logging.Logger
	messenger     conversation.ReplyMessenger
	messengerNote string
	jobUpdater    conversation.JobUpdater
	memoryQueue   *conversation.MemoryQueue
	dbPool        *pgxpool.Pool
	sqlDB         *sql.DB
	audit         *auditcompliance.AuditService
	outboxStore   *events.OutboxStore
	resolver      payments.OrgNumberResolver
	optOutChecker conversation.OptOutChecker
	supervisor    conversation.Supervisor
	redisClient   *redis.Client
	smsTranscript *conversation.SMSTranscriptStore
}

// setupInlineWorker builds and starts the in-process conversation worker.
func setupInlineWorker(deps inlineWorkerDeps) (*conversation.Worker, conversation.Service) {
	cfg := deps.cfg
	logger := deps.logger

	if !cfg.UseMemoryQueue || deps.memoryQueue == nil {
		return nil, nil
	}

	leadsRepo := initializeLeadsRepository(deps.dbPool)
	var paymentChecker *payments.Repository
	if deps.dbPool != nil {
		paymentChecker = payments.NewRepository(deps.dbPool, deps.redisClient)
	}

	processor, err := appbootstrap.BuildConversationService(deps.ctx, cfg, leadsRepo, paymentChecker, deps.audit, logger)
	if err != nil {
		logger.Error("failed to configure inline conversation service", "error", err)
		os.Exit(1)
	}

	if deps.messenger == nil {
		logger.Warn("SMS replies disabled for inline workers", "reason", deps.messengerNote)
	}

	var bookingBridge conversation.BookingServiceAdapter
	if deps.dbPool != nil {
		repo := bookings.NewRepository(deps.dbPool)
		bookingBridge = conversation.BookingServiceAdapter{
			Service: bookings.NewService(repo, logger),
		}
	}

	var clinicStore *clinic.Store
	if deps.redisClient != nil {
		clinicStore = clinic.NewStore(deps.redisClient)
	}

	var convStore *conversation.ConversationStore
	if cfg.PersistConversationHistory {
		convStore = conversation.NewConversationStore(deps.sqlDB)
	}

	deposit := buildDepositSender(depositDeps{
		cfg:            cfg,
		logger:         logger,
		dbPool:         deps.dbPool,
		outboxStore:    deps.outboxStore,
		paymentChecker: paymentChecker,
		messenger:      deps.messenger,
		resolver:       deps.resolver,
		leadsRepo:      leadsRepo,
		smsTranscript:  deps.smsTranscript,
		convStore:      convStore,
		clinicStore:    clinicStore,
	})

	notifier := buildNotificationService(deps.ctx, cfg, logger, clinicStore, leadsRepo, deps.messenger)
	autoPurger := buildAutoPurger(cfg, logger, deps.dbPool, deps.redisClient)

	var processedStore *events.ProcessedStore
	if deps.dbPool != nil {
		processedStore = events.NewProcessedStore(deps.dbPool)
	}

	var msgChecker conversation.ProviderMessageChecker
	if deps.optOutChecker != nil {
		if checker, ok := deps.optOutChecker.(conversation.ProviderMessageChecker); ok {
			msgChecker = checker
		}
	}

	workerOpts := buildWorkerOptions(cfg, deposit, notifier, autoPurger, processedStore, deps.optOutChecker, msgChecker, clinicStore, deps.smsTranscript, convStore, deps.supervisor, leadsRepo, logger)

	worker := conversation.NewWorker(processor, deps.memoryQueue, deps.jobUpdater, deps.messenger, bookingBridge, logger, workerOpts...)
	worker.Start(deps.ctx)
	logger.Info("inline conversation workers started", "count", cfg.WorkerCount)
	return worker, processor
}

// buildWorkerOptions assembles the worker option list.
func buildWorkerOptions(
	cfg *appconfig.Config,
	deposit depositResult,
	notifier conversation.PaymentNotifier,
	autoPurger conversation.SandboxAutoPurger,
	processedStore *events.ProcessedStore,
	optOutChecker conversation.OptOutChecker,
	msgChecker conversation.ProviderMessageChecker,
	clinicStore *clinic.Store,
	smsTranscript *conversation.SMSTranscriptStore,
	convStore *conversation.ConversationStore,
	supervisor conversation.Supervisor,
	leadsRepo leads.Repository,
	logger *logging.Logger,
) []conversation.WorkerOption {
	moxieDryRun := os.Getenv("MOXIE_DRY_RUN") == "true"
	moxieAPIClient := moxieclient.NewClient(logger, moxieclient.WithDryRun(moxieDryRun))
	if moxieDryRun {
		logger.Info("Moxie API in DRY RUN mode — no real appointments")
	}

	return []conversation.WorkerOption{
		conversation.WithWorkerCount(cfg.WorkerCount),
		conversation.WithDepositSender(deposit.sender),
		conversation.WithDepositPreloader(deposit.preloader),
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
		conversation.WithWorkerMoxieClient(moxieAPIClient),
	}
}
