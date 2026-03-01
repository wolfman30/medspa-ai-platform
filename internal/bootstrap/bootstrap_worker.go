package bootstrap

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

// DepositDeps holds everything needed to construct a deposit sender.
type DepositDeps struct {
	Cfg            *appconfig.Config
	Logger         *logging.Logger
	DBPool         *pgxpool.Pool
	OutboxStore    *events.OutboxStore
	PaymentChecker *payments.Repository
	Messenger      conversation.ReplyMessenger
	Resolver       payments.OrgNumberResolver
	LeadsRepo      leads.Repository
	SMSTranscript  *conversation.SMSTranscriptStore
	ConvStore      *conversation.ConversationStore
	ClinicStore    *clinic.Store
}

// DepositResult holds the outputs of BuildDepositSender.
type DepositResult struct {
	Sender    conversation.DepositSender
	Preloader *conversation.DepositPreloader
}

// BuildDepositSender selects the correct payment provider (Fake, Stripe-only,
// Square, or Multi) and wires the deposit dispatcher.
func BuildDepositSender(deps DepositDeps) DepositResult {
	cfg := deps.Cfg
	logger := deps.Logger

	if deps.DBPool == nil || deps.OutboxStore == nil || deps.PaymentChecker == nil {
		logger.Warn("deposit sender NOT initialized — missing prerequisites")
		return DepositResult{}
	}

	numberResolver := deps.Resolver
	hasSquare := strings.TrimSpace(cfg.SquareAccessToken) != "" ||
		(cfg.SquareClientID != "" && cfg.SquareClientSecret != "" && cfg.SquareOAuthRedirectURI != "")
	hasStripe := cfg.StripeSecretKey != ""

	// Fake payments mode: only when Square isn't configured
	if cfg.AllowFakePayments && !hasSquare {
		fakeSvc := payments.NewFakeCheckoutService(cfg.PublicBaseURL, logger)
		logger.Warn("deposit sender initialized in fake payments mode")
		return DepositResult{
			Sender: conversation.NewDepositDispatcher(deps.PaymentChecker, fakeSvc, deps.OutboxStore, deps.Messenger, numberResolver, deps.LeadsRepo, deps.SMSTranscript, deps.ConvStore, logger, conversation.WithShortURLs(deps.PaymentChecker, cfg.PublicBaseURL)),
		}
	}

	// Stripe-only mode
	if !hasSquare && hasStripe {
		stripeSvc := payments.NewStripeCheckoutService(cfg.StripeSecretKey, cfg.StripeSuccessURL, cfg.StripeCancelURL, logger)
		logger.Info("deposit sender initialized (stripe only)")
		return DepositResult{
			Sender: conversation.NewDepositDispatcher(deps.PaymentChecker, stripeSvc, deps.OutboxStore, deps.Messenger, numberResolver, deps.LeadsRepo, deps.SMSTranscript, deps.ConvStore, logger, conversation.WithShortURLs(deps.PaymentChecker, cfg.PublicBaseURL)),
		}
	}

	// No provider at all
	if !hasSquare && !hasStripe {
		logger.Warn("deposit sender NOT initialized — no payment provider configured")
		return DepositResult{}
	}

	// Square (possibly with Stripe multi-checkout)
	return buildSquareDepositSender(deps, numberResolver)
}

// buildSquareDepositSender handles the Square + optional Stripe multi-checkout path.
func buildSquareDepositSender(deps DepositDeps, numberResolver payments.OrgNumberResolver) DepositResult {
	cfg := deps.Cfg
	logger := deps.Logger

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
			deps.DBPool,
			logger,
		)
		squareSvc = squareSvc.WithCredentialsProvider(oauthSvc)
		numberResolver = payments.NewDBOrgNumberResolver(oauthSvc, numberResolver)
		logger.Info("square oauth wired into inline workers", "sandbox", cfg.SquareSandbox)
	}

	var checkoutSvc payments.CheckoutProvider = squareSvc
	if cfg.StripeSecretKey != "" && deps.ClinicStore != nil {
		stripeSvc := payments.NewStripeCheckoutService(cfg.StripeSecretKey, cfg.StripeSuccessURL, cfg.StripeCancelURL, logger)
		checkoutSvc = payments.NewMultiCheckoutService(squareSvc, stripeSvc, deps.ClinicStore, logger)
		logger.Info("multi-checkout service initialized (square + stripe)")
	}

	preloader := conversation.NewDepositPreloader(squareSvc, 5000, logger)
	logger.Info("deposit sender initialized", "square_location_id", cfg.SquareLocationID)

	return DepositResult{
		Sender:    conversation.NewDepositDispatcher(deps.PaymentChecker, checkoutSvc, deps.OutboxStore, deps.Messenger, numberResolver, deps.LeadsRepo, deps.SMSTranscript, deps.ConvStore, logger, conversation.WithShortURLs(deps.PaymentChecker, cfg.PublicBaseURL)),
		Preloader: preloader,
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

// InlineWorkerDeps holds everything SetupInlineWorker needs.
type InlineWorkerDeps struct {
	Ctx           context.Context
	Cfg           *appconfig.Config
	Logger        *logging.Logger
	Messenger     conversation.ReplyMessenger
	MessengerNote string
	JobUpdater    conversation.JobUpdater
	MemoryQueue   *conversation.MemoryQueue
	DBPool        *pgxpool.Pool
	SQLDB         *sql.DB
	Audit         *auditcompliance.AuditService
	OutboxStore   *events.OutboxStore
	Resolver      payments.OrgNumberResolver
	OptOutChecker conversation.OptOutChecker
	Supervisor    conversation.Supervisor
	RedisClient   *redis.Client
	SMSTranscript *conversation.SMSTranscriptStore
}

// SetupInlineWorker builds and starts the in-process conversation worker.
func SetupInlineWorker(deps InlineWorkerDeps) (*conversation.Worker, conversation.Service) {
	cfg := deps.Cfg
	logger := deps.Logger

	if !cfg.UseMemoryQueue || deps.MemoryQueue == nil {
		return nil, nil
	}

	leadsRepo := initializeLeadsRepository(deps.DBPool)
	var paymentChecker *payments.Repository
	if deps.DBPool != nil {
		paymentChecker = payments.NewRepository(deps.DBPool, deps.RedisClient)
	}

	processor, err := appbootstrap.BuildConversationService(deps.Ctx, cfg, leadsRepo, paymentChecker, deps.Audit, logger)
	if err != nil {
		logger.Error("failed to configure inline conversation service", "error", err)
		os.Exit(1)
	}

	if deps.Messenger == nil {
		logger.Warn("SMS replies disabled for inline workers", "reason", deps.MessengerNote)
	}

	var bookingBridge conversation.BookingServiceAdapter
	if deps.DBPool != nil {
		repo := bookings.NewRepository(deps.DBPool)
		bookingBridge = conversation.BookingServiceAdapter{
			Service: bookings.NewService(repo, logger),
		}
	}

	var clinicStore *clinic.Store
	if deps.RedisClient != nil {
		clinicStore = clinic.NewStore(deps.RedisClient)
	}

	var convStore *conversation.ConversationStore
	if cfg.PersistConversationHistory {
		convStore = conversation.NewConversationStore(deps.SQLDB)
	}

	deposit := BuildDepositSender(DepositDeps{
		Cfg:            cfg,
		Logger:         logger,
		DBPool:         deps.DBPool,
		OutboxStore:    deps.OutboxStore,
		PaymentChecker: paymentChecker,
		Messenger:      deps.Messenger,
		Resolver:       deps.Resolver,
		LeadsRepo:      leadsRepo,
		SMSTranscript:  deps.SMSTranscript,
		ConvStore:      convStore,
		ClinicStore:    clinicStore,
	})

	notifier := buildNotificationService(deps.Ctx, cfg, logger, clinicStore, leadsRepo, deps.Messenger)
	autoPurger := buildAutoPurger(cfg, logger, deps.DBPool, deps.RedisClient)

	var processedStore *events.ProcessedStore
	if deps.DBPool != nil {
		processedStore = events.NewProcessedStore(deps.DBPool)
	}

	var msgChecker conversation.ProviderMessageChecker
	if deps.OptOutChecker != nil {
		if checker, ok := deps.OptOutChecker.(conversation.ProviderMessageChecker); ok {
			msgChecker = checker
		}
	}

	workerOpts := buildWorkerOptions(cfg, deposit, notifier, autoPurger, processedStore, deps.OptOutChecker, msgChecker, clinicStore, deps.SMSTranscript, convStore, deps.Supervisor, leadsRepo, logger)

	worker := conversation.NewWorker(processor, deps.MemoryQueue, deps.JobUpdater, deps.Messenger, bookingBridge, logger, workerOpts...)
	worker.Start(deps.Ctx)
	logger.Info("inline conversation workers started", "count", cfg.WorkerCount)
	return worker, processor
}

// buildWorkerOptions assembles the worker option list.
func buildWorkerOptions(
	cfg *appconfig.Config,
	deposit DepositResult,
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
		conversation.WithDepositSender(deposit.Sender),
		conversation.WithDepositPreloader(deposit.Preloader),
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
