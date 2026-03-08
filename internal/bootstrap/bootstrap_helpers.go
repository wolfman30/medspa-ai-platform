package bootstrap

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	auditcompliance "github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/internal/voice"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type ClinicBootstrap struct {
	RedisClient   *redis.Client
	ClinicStore   *clinic.Store
	SMSTranscript *conversation.SMSTranscriptStore
}

func BootstrapClinic(cfg *appconfig.Config, appCtx context.Context, logger *logging.Logger) ClinicBootstrap {
	redisClient := appbootstrap.BuildRedisClient(appCtx, cfg, logger, false)
	clinicStore := appbootstrap.BuildClinicStore(redisClient)
	smsTranscript := appbootstrap.BuildSMSTranscriptStore(redisClient)
	return ClinicBootstrap{RedisClient: redisClient, ClinicStore: clinicStore, SMSTranscript: smsTranscript}
}

type MessagingDeps struct {
	Cfg                   *appconfig.Config
	Logger                *logging.Logger
	ConversationPublisher *conversation.Publisher
	LeadsRepo             leads.Repository
	MessageStore          *messaging.Store
	AuditService          *auditcompliance.AuditService
	ConversationStore     *conversation.ConversationStore
	SMSTranscriptStore    *conversation.SMSTranscriptStore
	ClinicStore           *clinic.Store
}

type MessagingBootstrap struct {
	MessagingHandler *messaging.Handler
	Resolver         *messaging.StaticOrgResolver
	WebhookMessenger conversation.ReplyMessenger
	MessengerReason  string
}

func BootstrapMessaging(deps MessagingDeps) MessagingBootstrap {
	cfg := deps.Cfg
	logger := deps.Logger
	conversationPublisher := deps.ConversationPublisher
	leadsRepo := deps.LeadsRepo
	msgStore := deps.MessageStore
	auditSvc := deps.AuditService
	conversationStore := deps.ConversationStore
	smsTranscript := deps.SMSTranscriptStore
	clinicStore := deps.ClinicStore
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

	if cfg.TwilioSkipSignature && (cfg.Env == "production" || cfg.Env == "staging") {
		logger.Error("SECURITY WARNING: TWILIO_SKIP_SIGNATURE is enabled in production/staging - this is a security risk!")
	}

	return MessagingBootstrap{MessagingHandler: messagingHandler, Resolver: resolver, WebhookMessenger: webhookMessenger, MessengerReason: webhookMessengerReason}
}

type PaymentsBootstrap struct {
	CheckoutHandler      *payments.CheckoutHandler
	SquareWebhookHandler *payments.SquareWebhookHandler
	SquareOAuthHandler   *payments.OAuthHandler
	FakePaymentsHandler  *payments.FakePaymentsHandler
	StripeWebhookHandler *payments.StripeWebhookHandler
	StripeConnectHandler *payments.StripeConnectHandler
}

type PaymentsDeps struct {
	AppCtx                context.Context
	Cfg                   *appconfig.Config
	Logger                *logging.Logger
	DBPool                *pgxpool.Pool
	LeadsRepo             leads.Repository
	RedisClient           *redis.Client
	OutboxStore           *events.OutboxStore
	ProcessedStore        *events.ProcessedStore
	Resolver              payments.OrgNumberResolver
	PaymentsRepo          *payments.Repository
	ClinicStore           *clinic.Store
	ConversationPublisher *conversation.Publisher
}

func BootstrapPayments(deps PaymentsDeps) PaymentsBootstrap {
	appCtx := deps.AppCtx
	cfg := deps.Cfg
	logger := deps.Logger
	dbPool := deps.DBPool
	leadsRepo := deps.LeadsRepo
	outboxStore := deps.OutboxStore
	processedStore := deps.ProcessedStore
	resolver := deps.Resolver
	paymentsRepo := deps.PaymentsRepo
	clinicStore := deps.ClinicStore
	conversationPublisher := deps.ConversationPublisher
	_ = deps.RedisClient

	var checkoutHandler *payments.CheckoutHandler
	var squareWebhookHandler *payments.SquareWebhookHandler
	var squareOAuthHandler *payments.OAuthHandler
	var fakePaymentsHandler *payments.FakePaymentsHandler
	if paymentsRepo != nil && processedStore != nil && outboxStore != nil {
		var oauthSvc *payments.SquareOAuthService
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

			tokenRefreshWorker := payments.NewTokenRefreshWorker(oauthSvc, logger)
			go tokenRefreshWorker.Start(appCtx)

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

	return PaymentsBootstrap{
		CheckoutHandler:      checkoutHandler,
		SquareWebhookHandler: squareWebhookHandler,
		SquareOAuthHandler:   squareOAuthHandler,
		FakePaymentsHandler:  fakePaymentsHandler,
		StripeWebhookHandler: stripeWebhookHandler,
		StripeConnectHandler: stripeConnectHandler,
	}
}

type VoiceBootstrap struct {
	VoiceAIHandler *handlers.VoiceAIHandler
	VoiceWSHandler *voice.TelnyxWSHandler
	CallControl    *handlers.CallControlHandler
}

type VoiceDeps struct {
	Cfg                   *appconfig.Config
	Logger                *logging.Logger
	MsgStore              *messaging.Store
	ClinicStore           *clinic.Store
	ConversationPublisher *conversation.Publisher
	ConversationService   conversation.Service
	ConversationStore     *conversation.ConversationStore
	RedisClient           *redis.Client
	WebhookMessenger      conversation.ReplyMessenger
	LeadsRepo             leads.Repository
	Resolver              *messaging.StaticOrgResolver
}

func BootstrapVoice(deps VoiceDeps) VoiceBootstrap {
	cfg := deps.Cfg
	logger := deps.Logger
	msgStore := deps.MsgStore
	clinicStore := deps.ClinicStore
	conversationPublisher := deps.ConversationPublisher
	conversationService := deps.ConversationService
	conversationStore := deps.ConversationStore
	redisClient := deps.RedisClient
	webhookMessenger := deps.WebhookMessenger
	leadsRepo := deps.LeadsRepo
	resolver := deps.Resolver

	var voiceAIHandler *handlers.VoiceAIHandler
	if msgStore != nil && clinicStore != nil {
		voiceAIHandler = handlers.NewVoiceAIHandler(handlers.VoiceAIHandlerConfig{
			Store:       msgStore,
			Publisher:   conversationPublisher,
			Processor:   conversationService,
			ClinicStore: clinicStore,
			ConvStore:   conversationStore,
			Redis:       redisClient,
			Logger:      logger,
		})
		logger.Info("voice AI handler initialized")
	}

	var voiceWSHandler *voice.TelnyxWSHandler
	if cfg.NovaSonicSidecarURL != "" {
		sidecarURL := cfg.NovaSonicSidecarURL
		novaSonicVoice := cfg.NovaSonicVoice
		voiceToolDeps := &voice.ToolDeps{
			MoxieClient: func() *moxieclient.Client {
				moxieDryRun := os.Getenv("MOXIE_DRY_RUN") == "true"
				return moxieclient.NewClient(logger, moxieclient.WithDryRun(moxieDryRun))
			}(),
			Messenger:   webhookMessenger,
			ClinicStore: clinicStore,
			LeadsRepo:   leadsRepo,
		}

		voiceWSHandler = voice.NewTelnyxWSHandler(slog.Default(), func(l *slog.Logger, callControlID string, mediaFormat voice.TelnyxMediaFormat, callCtx voice.CallContext) (*voice.Bridge, error) {
			availabilitySummary := ""
			if voiceToolDeps.MoxieClient != nil {
				availabilitySummary = voice.FetchAvailabilitySummary(l, voiceToolDeps.MoxieClient, voiceToolDeps.ClinicStore, callCtx.OrgID)
			}

			systemPrompt := voice.BuildVoiceSystemPrompt(l, voiceToolDeps.ClinicStore, callCtx.OrgID, availabilitySummary)

			// Resolve clinic name for ElevenLabs greeting
			clinicName := "our office"
			if voiceToolDeps.ClinicStore != nil {
				if cfg, err := voiceToolDeps.ClinicStore.Get(context.Background(), callCtx.OrgID); err == nil && cfg != nil && cfg.Name != "" {
					clinicName = cfg.Name
				}
			}

			return voice.NewBridge(
				context.Background(),
				voice.BridgeConfig{
					SidecarURL:   sidecarURL,
					SystemPrompt: systemPrompt,
					Voice:        novaSonicVoice,
					OrgID:        callCtx.OrgID,
					CallerPhone:  callCtx.From,
					ClinicName:   clinicName,
					ToolDeps:     voiceToolDeps,
				},
				callControlID,
				mediaFormat,
				l,
			)
		})
		logger.Info("nova sonic voice WebSocket handler initialized", "sidecar_url", sidecarURL)
	}

	var callControlHandler *handlers.CallControlHandler
	if cfg.NovaSonicStreamURL != "" && cfg.TelnyxAPIKey != "" {
		callControlHandler = handlers.NewCallControlHandler(handlers.CallControlConfig{
			Logger:       logger,
			TelnyxAPIKey: cfg.TelnyxAPIKey,
			StreamURL:    cfg.NovaSonicStreamURL,
			OrgResolver:  resolver,
			ClinicStore:  clinicStore,
		})
		logger.Info("call control handler initialized", "stream_url", cfg.NovaSonicStreamURL)
	}

	return VoiceBootstrap{VoiceAIHandler: voiceAIHandler, VoiceWSHandler: voiceWSHandler, CallControl: callControlHandler}
}

func BootstrapNotifications(cfg *appconfig.Config, logger *logging.Logger) *handlers.GitHubWebhookHandler {
	var githubWebhookHandler *handlers.GitHubWebhookHandler
	if cfg.GitHubWebhookSecret != "" {
		githubNotifier := handlers.NewTelegramNotifier(cfg.TelegramBotToken, cfg.AndrewTelegramChatID, logger)
		githubWebhookHandler = handlers.NewGitHubWebhookHandler(cfg.GitHubWebhookSecret, githubNotifier, logger)
		logger.Info("github webhook handler initialized")
	} else {
		logger.Warn("github webhook handler not initialized (GITHUB_WEBHOOK_SECRET missing)")
	}
	return githubWebhookHandler
}
