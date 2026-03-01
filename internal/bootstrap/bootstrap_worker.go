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

// DepositPipeline holds the outputs of buildDepositSender.
type DepositPipeline struct {
	Sender    conversation.DepositSender
	Preloader *conversation.DepositPreloader
}

// ConversationWorkerAssembler groups shared dependencies for inline worker assembly.
type ConversationWorkerAssembler struct {
	ctx           context.Context
	cfg           *appconfig.Config
	logger        *logging.Logger
	dbPool        *pgxpool.Pool
	sqlDB         *sql.DB
	redisClient   *redis.Client
	messenger     conversation.ReplyMessenger
	outboxStore   *events.OutboxStore
	resolver      payments.OrgNumberResolver
	optOutChecker conversation.OptOutChecker
	supervisor    conversation.Supervisor
	smsTranscript *conversation.SMSTranscriptStore
	leadsRepo     leads.Repository
	paymentRepo   *payments.Repository
	clinicStore   *clinic.Store
	convStore     *conversation.ConversationStore
}

type ConversationWorkerAssemblerDeps struct {
	InlineWorkerDeps  InlineWorkerDeps
	LeadsRepo         leads.Repository
	PaymentRepo       *payments.Repository
	ClinicStore       *clinic.Store
	ConversationStore *conversation.ConversationStore
}

func NewConversationWorkerAssembler(deps ConversationWorkerAssemblerDeps) *ConversationWorkerAssembler {
	inlineWorkerDeps := deps.InlineWorkerDeps
	return &ConversationWorkerAssembler{
		ctx:           inlineWorkerDeps.Ctx,
		cfg:           inlineWorkerDeps.Cfg,
		logger:        inlineWorkerDeps.Logger,
		dbPool:        inlineWorkerDeps.DBPool,
		sqlDB:         inlineWorkerDeps.SQLDB,
		redisClient:   inlineWorkerDeps.RedisClient,
		messenger:     inlineWorkerDeps.Messenger,
		outboxStore:   inlineWorkerDeps.OutboxStore,
		resolver:      inlineWorkerDeps.Resolver,
		optOutChecker: inlineWorkerDeps.OptOutChecker,
		supervisor:    inlineWorkerDeps.Supervisor,
		smsTranscript: inlineWorkerDeps.SMSTranscript,
		leadsRepo:     deps.LeadsRepo,
		paymentRepo:   deps.PaymentRepo,
		clinicStore:   deps.ClinicStore,
		convStore:     deps.ConversationStore,
	}
}

// buildDepositSender selects the correct payment provider (Fake, Stripe-only,
// Square, or Multi) and wires the deposit dispatcher.
func (a *ConversationWorkerAssembler) buildDepositSender() DepositPipeline {
	if a.dbPool == nil || a.outboxStore == nil || a.paymentRepo == nil {
		a.logger.Warn("deposit sender NOT initialized — missing prerequisites")
		return DepositPipeline{}
	}

	numberResolver := a.resolver
	hasSquare := strings.TrimSpace(a.cfg.SquareAccessToken) != "" ||
		(a.cfg.SquareClientID != "" && a.cfg.SquareClientSecret != "" && a.cfg.SquareOAuthRedirectURI != "")
	hasStripe := a.cfg.StripeSecretKey != ""

	// Fake payments mode: only when Square isn't configured
	if a.cfg.AllowFakePayments && !hasSquare {
		fakeSvc := payments.NewFakeCheckoutService(a.cfg.PublicBaseURL, a.logger)
		a.logger.Warn("deposit sender initialized in fake payments mode")
		return DepositPipeline{
			Sender: conversation.NewDepositDispatcher(a.paymentRepo, fakeSvc, a.outboxStore, a.messenger, numberResolver, a.leadsRepo, a.smsTranscript, a.convStore, a.logger, conversation.WithShortURLs(a.paymentRepo, a.cfg.PublicBaseURL)),
		}
	}

	// Stripe-only mode
	if !hasSquare && hasStripe {
		stripeSvc := payments.NewStripeCheckoutService(a.cfg.StripeSecretKey, a.cfg.StripeSuccessURL, a.cfg.StripeCancelURL, a.logger)
		a.logger.Info("deposit sender initialized (stripe only)")
		return DepositPipeline{
			Sender: conversation.NewDepositDispatcher(a.paymentRepo, stripeSvc, a.outboxStore, a.messenger, numberResolver, a.leadsRepo, a.smsTranscript, a.convStore, a.logger, conversation.WithShortURLs(a.paymentRepo, a.cfg.PublicBaseURL)),
		}
	}

	// No provider at all
	if !hasSquare && !hasStripe {
		a.logger.Warn("deposit sender NOT initialized — no payment provider configured")
		return DepositPipeline{}
	}

	// Square (possibly with Stripe multi-checkout)
	return a.buildSquareDepositSender(numberResolver)
}

// buildSquareDepositSender handles the Square + optional Stripe multi-checkout path.
func (a *ConversationWorkerAssembler) buildSquareDepositSender(numberResolver payments.OrgNumberResolver) DepositPipeline {
	usePaymentLinks := payments.UsePaymentLinks(a.cfg.SquareCheckoutMode, a.cfg.SquareSandbox)
	squareSvc := payments.NewSquareCheckoutService(a.cfg.SquareAccessToken, a.cfg.SquareLocationID, a.cfg.SquareSuccessURL, a.cfg.SquareCancelURL, a.logger).
		WithBaseURL(a.cfg.SquareBaseURL).
		WithPaymentLinks(usePaymentLinks).
		WithPaymentLinkFallback(a.cfg.SquareCheckoutAllowFallback)

	if a.cfg.SquareClientID != "" && a.cfg.SquareClientSecret != "" && a.cfg.SquareOAuthRedirectURI != "" {
		oauthSvc := payments.NewSquareOAuthService(
			payments.SquareOAuthConfig{
				ClientID:     a.cfg.SquareClientID,
				ClientSecret: a.cfg.SquareClientSecret,
				RedirectURI:  a.cfg.SquareOAuthRedirectURI,
				Sandbox:      a.cfg.SquareSandbox,
			},
			a.dbPool,
			a.logger,
		)
		squareSvc = squareSvc.WithCredentialsProvider(oauthSvc)
		numberResolver = payments.NewDBOrgNumberResolver(oauthSvc, numberResolver)
		a.logger.Info("square oauth wired into inline workers", "sandbox", a.cfg.SquareSandbox)
	}

	var checkoutSvc payments.CheckoutProvider = squareSvc
	if a.cfg.StripeSecretKey != "" && a.clinicStore != nil {
		stripeSvc := payments.NewStripeCheckoutService(a.cfg.StripeSecretKey, a.cfg.StripeSuccessURL, a.cfg.StripeCancelURL, a.logger)
		checkoutSvc = payments.NewMultiCheckoutService(squareSvc, stripeSvc, a.clinicStore, a.logger)
		a.logger.Info("multi-checkout service initialized (square + stripe)")
	}

	preloader := conversation.NewDepositPreloader(squareSvc, 5000, a.logger)
	a.logger.Info("deposit sender initialized", "square_location_id", a.cfg.SquareLocationID)

	return DepositPipeline{
		Sender:    conversation.NewDepositDispatcher(a.paymentRepo, checkoutSvc, a.outboxStore, a.messenger, numberResolver, a.leadsRepo, a.smsTranscript, a.convStore, a.logger, conversation.WithShortURLs(a.paymentRepo, a.cfg.PublicBaseURL)),
		Preloader: preloader,
	}
}

// buildNotificationService constructs the email + SMS notification pipeline.
func (a *ConversationWorkerAssembler) buildNotificationService() conversation.PaymentNotifier {
	if a.clinicStore == nil {
		a.logger.Warn("notification service NOT initialized (redis not configured)")
		return nil
	}

	emailSender := a.buildEmailSender()
	smsSender := a.buildSMSSender()

	notifier := notify.NewService(emailSender, smsSender, a.clinicStore, a.leadsRepo, a.logger)
	a.logger.Info("notification service initialized for inline workers")
	return notifier
}

// buildEmailSender picks SES → SendGrid → Stub in priority order.
func (a *ConversationWorkerAssembler) buildEmailSender() notify.EmailSender {
	if a.cfg.SESFromEmail != "" {
		sesAwsCfg, err := mainconfig.LoadAWSConfig(a.ctx, a.cfg)
		if err != nil {
			a.logger.Error("failed to load AWS config for SES", "error", err)
		} else {
			sesClient := sesv2.NewFromConfig(sesAwsCfg)
			a.logger.Info("AWS SES email sender initialized", "from", a.cfg.SESFromEmail)
			return notify.NewSESSender(sesClient, notify.SESConfig{
				FromEmail: a.cfg.SESFromEmail,
				FromName:  a.cfg.SESFromName,
			}, a.logger)
		}
	}

	if a.cfg.SendGridAPIKey != "" && a.cfg.SendGridFromEmail != "" {
		a.logger.Info("sendgrid email sender initialized")
		return notify.NewSendGridSender(notify.SendGridConfig{
			APIKey:    a.cfg.SendGridAPIKey,
			FromEmail: a.cfg.SendGridFromEmail,
			FromName:  a.cfg.SendGridFromName,
		}, a.logger)
	}

	a.logger.Warn("email notifications disabled (SES_FROM_EMAIL or SENDGRID_API_KEY not set)")
	return notify.NewStubEmailSender(a.logger)
}

// buildSMSSender creates an operator SMS sender using the outbound messenger.
func (a *ConversationWorkerAssembler) buildSMSSender() notify.SMSSender {
	smsFromNumber := a.cfg.TelnyxFromNumber
	if smsFromNumber == "" {
		smsFromNumber = a.cfg.TwilioFromNumber
	}

	if a.messenger != nil && smsFromNumber != "" {
		a.logger.Info("sms sender initialized for operator notifications", "from", smsFromNumber)
		return notify.NewSimpleSMSSender(smsFromNumber, func(ctx context.Context, to, from, body string) error {
			return a.messenger.SendReply(ctx, conversation.OutboundReply{
				To:   to,
				From: from,
				Body: body,
			})
		}, a.logger)
	}

	a.logger.Warn("operator SMS notifications disabled (messenger not available or no from number)")
	return notify.NewStubSMSSender(a.logger)
}

// buildAutoPurger creates a sandbox auto-purger if configured.
func (a *ConversationWorkerAssembler) buildAutoPurger() conversation.SandboxAutoPurger {
	if a.cfg.Env == "production" || !a.cfg.SquareSandbox || a.dbPool == nil {
		return nil
	}

	phones := clinicdata.ParsePhoneDigitsList(a.cfg.SandboxAutoPurgePhones)
	if len(phones) == 0 {
		return nil
	}

	purger := clinicdata.NewPurger(a.dbPool, a.redisClient, a.logger)
	a.logger.Info("sandbox auto purge enabled", "delay", a.cfg.SandboxAutoPurgeDelay.String())
	return clinicdata.NewSandboxAutoPurger(purger, clinicdata.AutoPurgeConfig{
		Enabled:            true,
		AllowedPhoneDigits: phones,
		Delay:              a.cfg.SandboxAutoPurgeDelay,
	}, a.logger)
}

// buildConversationWorkerOptions assembles the worker option list.
func (a *ConversationWorkerAssembler) buildConversationWorkerOptions() []conversation.WorkerOption {
	deposit := a.buildDepositSender()
	notifier := a.buildNotificationService()
	autoPurger := a.buildAutoPurger()
	var processedStore *events.ProcessedStore
	if a.dbPool != nil {
		processedStore = events.NewProcessedStore(a.dbPool)
	}
	var msgChecker conversation.ProviderMessageChecker
	if a.optOutChecker != nil {
		if checker, ok := a.optOutChecker.(conversation.ProviderMessageChecker); ok {
			msgChecker = checker
		}
	}
	moxieDryRun := os.Getenv("MOXIE_DRY_RUN") == "true"
	moxieAPIClient := moxieclient.NewClient(a.logger, moxieclient.WithDryRun(moxieDryRun))
	if moxieDryRun {
		a.logger.Info("Moxie API in DRY RUN mode — no real appointments")
	}

	return []conversation.WorkerOption{
		conversation.WithWorkerCount(a.cfg.WorkerCount),
		conversation.WithDepositSender(deposit.Sender),
		conversation.WithDepositPreloader(deposit.Preloader),
		conversation.WithPaymentNotifier(notifier),
		conversation.WithSandboxAutoPurger(autoPurger),
		conversation.WithProcessedEventsStore(processedStore),
		conversation.WithOptOutChecker(a.optOutChecker),
		conversation.WithProviderMessageChecker(msgChecker),
		conversation.WithClinicConfigStore(a.clinicStore),
		conversation.WithSMSTranscriptStore(a.smsTranscript),
		conversation.WithConversationStore(a.convStore),
		conversation.WithSupervisor(a.supervisor),
		conversation.WithSupervisorMode(conversation.ParseSupervisorMode(a.cfg.SupervisorMode)),
		conversation.WithWorkerLeadsRepo(a.leadsRepo),
		conversation.WithWorkerMoxieClient(moxieAPIClient),
	}
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

	assembler := NewConversationWorkerAssembler(ConversationWorkerAssemblerDeps{
		InlineWorkerDeps:  deps,
		LeadsRepo:         leadsRepo,
		PaymentRepo:       paymentChecker,
		ClinicStore:       clinicStore,
		ConversationStore: convStore,
	})

	workerOpts := assembler.buildConversationWorkerOptions()

	worker := conversation.NewWorker(processor, deps.MemoryQueue, deps.JobUpdater, deps.Messenger, bookingBridge, logger, workerOpts...)
	worker.Start(deps.Ctx)
	logger.Info("inline conversation workers started", "count", cfg.WorkerCount)
	return worker, processor
}
