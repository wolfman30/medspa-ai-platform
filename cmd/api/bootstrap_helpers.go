package main

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
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/internal/voice"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type clinicBootstrap struct {
	redisClient   *redis.Client
	clinicStore   *clinic.Store
	smsTranscript *conversation.SMSTranscriptStore
}

func bootstrapClinic(cfg *appconfig.Config, appCtx context.Context, logger *logging.Logger) clinicBootstrap {
	redisClient := appbootstrap.BuildRedisClient(appCtx, cfg, logger, false)
	clinicStore := appbootstrap.BuildClinicStore(redisClient)
	smsTranscript := appbootstrap.BuildSMSTranscriptStore(redisClient)
	return clinicBootstrap{redisClient: redisClient, clinicStore: clinicStore, smsTranscript: smsTranscript}
}

type messagingBootstrap struct {
	messagingHandler *messaging.Handler
	resolver         *messaging.StaticOrgResolver
	webhookMessenger conversation.ReplyMessenger
	messengerReason  string
}

func bootstrapMessaging(cfg *appconfig.Config, logger *logging.Logger, conversationPublisher *conversation.Publisher, leadsRepo leads.Repository, msgStore *messaging.Store, auditSvc *auditcompliance.AuditService, conversationStore *conversation.ConversationStore, smsTranscript *conversation.SMSTranscriptStore, clinicStore *clinic.Store) messagingBootstrap {
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

	return messagingBootstrap{messagingHandler: messagingHandler, resolver: resolver, webhookMessenger: webhookMessenger, messengerReason: webhookMessengerReason}
}

type paymentsBootstrap struct {
	checkoutHandler      *payments.CheckoutHandler
	squareWebhookHandler *payments.SquareWebhookHandler
	squareOAuthHandler   *payments.OAuthHandler
	fakePaymentsHandler  *payments.FakePaymentsHandler
	stripeWebhookHandler *payments.StripeWebhookHandler
	stripeConnectHandler *payments.StripeConnectHandler
}

func bootstrapPayments(appCtx context.Context, cfg *appconfig.Config, logger *logging.Logger, dbPool *pgxpool.Pool, leadsRepo leads.Repository, redisClient *redis.Client, outboxStore *events.OutboxStore, processedStore *events.ProcessedStore, resolver payments.OrgNumberResolver, paymentsRepo *payments.Repository, clinicStore *clinic.Store, conversationPublisher *conversation.Publisher) paymentsBootstrap {
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

	return paymentsBootstrap{
		checkoutHandler:      checkoutHandler,
		squareWebhookHandler: squareWebhookHandler,
		squareOAuthHandler:   squareOAuthHandler,
		fakePaymentsHandler:  fakePaymentsHandler,
		stripeWebhookHandler: stripeWebhookHandler,
		stripeConnectHandler: stripeConnectHandler,
	}
}

type voiceBootstrap struct {
	voiceAIHandler    *handlers.VoiceAIHandler
	voiceWSHandler    *voice.TelnyxWSHandler
	callControl       *handlers.CallControlHandler
}

func bootstrapVoice(cfg *appconfig.Config, logger *logging.Logger, msgStore *messaging.Store, clinicStore *clinic.Store, conversationPublisher *conversation.Publisher, conversationService conversation.Service, conversationStore *conversation.ConversationStore, redisClient *redis.Client, webhookMessenger conversation.ReplyMessenger, leadsRepo leads.Repository, resolver *messaging.StaticOrgResolver) voiceBootstrap {
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

			return voice.NewBridge(
				context.Background(),
				voice.BridgeConfig{
					SidecarURL:   sidecarURL,
					SystemPrompt: systemPrompt,
					Voice:        novaSonicVoice,
					OrgID:        callCtx.OrgID,
					CallerPhone:  callCtx.From,
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

	return voiceBootstrap{voiceAIHandler: voiceAIHandler, voiceWSHandler: voiceWSHandler, callControl: callControlHandler}
}

func bootstrapNotifications(cfg *appconfig.Config, logger *logging.Logger) *handlers.GitHubWebhookHandler {
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
