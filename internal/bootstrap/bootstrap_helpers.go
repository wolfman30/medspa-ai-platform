package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	appbootstrap "github.com/wolfman30/medspa-ai-platform/internal/app/bootstrap"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	auditcompliance "github.com/wolfman30/medspa-ai-platform/internal/compliance"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/emr/boulevard"
	moxieclient "github.com/wolfman30/medspa-ai-platform/internal/emr/moxie"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/internal/voice"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// ClinicBootstrap holds Redis-backed clinic configuration services.
type ClinicBootstrap struct {
	RedisClient   *redis.Client
	ClinicStore   *clinic.Store
	SMSTranscript *conversation.SMSTranscriptStore
}

// BootstrapClinic connects to Redis and initializes the clinic config store
// and SMS transcript store.
func BootstrapClinic(cfg *appconfig.Config, appCtx context.Context, logger *logging.Logger) ClinicBootstrap {
	redisClient := appbootstrap.BuildRedisClient(appCtx, cfg, logger, false)
	clinicStore := appbootstrap.BuildClinicStore(redisClient)
	smsTranscript := appbootstrap.BuildSMSTranscriptStore(redisClient)
	return ClinicBootstrap{RedisClient: redisClient, ClinicStore: clinicStore, SMSTranscript: smsTranscript}
}

// MessagingDeps holds dependencies for initializing SMS webhook handling
// and outbound messaging.
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

// MessagingBootstrap holds the assembled messaging handler, org resolver,
// and outbound messenger for SMS processing.
type MessagingBootstrap struct {
	MessagingHandler *messaging.Handler
	Resolver         *messaging.StaticOrgResolver
	WebhookMessenger conversation.ReplyMessenger
	MessengerReason  string
}

// BootstrapMessaging wires up the SMS webhook handler, org-to-number routing,
// and outbound messenger (Telnyx or Twilio).
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

// PaymentsBootstrap holds payment webhook handlers and checkout services
// for Square, Stripe, and fake/sandbox payment flows.
type PaymentsBootstrap struct {
	CheckoutHandler      *payments.CheckoutHandler
	SquareWebhookHandler *payments.SquareWebhookHandler
	SquareOAuthHandler   *payments.OAuthHandler
	FakePaymentsHandler  *payments.FakePaymentsHandler
	StripeWebhookHandler *payments.StripeWebhookHandler
	StripeConnectHandler *payments.StripeConnectHandler
}

// PaymentsDeps holds dependencies for initializing payment processing
// (Square OAuth, Stripe Connect, webhooks, and checkout).
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

// BootstrapPayments initializes Square and Stripe payment processing,
// including OAuth, webhooks, checkout services, and the multi-provider router.
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
	redisClient := deps.RedisClient

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
		if redisClient != nil {
			stripeWebhookHandler.SetRedis(redisClient)
		}
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

// VoiceBootstrap holds handlers for voice AI (Telnyx Call Control,
// Nova Sonic WebSocket bridge, and voice webhook processing).
type VoiceBootstrap struct {
	VoiceAIHandler *handlers.VoiceAIHandler
	VoiceWSHandler *voice.TelnyxWSHandler
	CallControl    *handlers.CallControlHandler
}

// VoiceDeps holds dependencies for initializing voice AI handlers.
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

// BootstrapVoice initializes the voice AI handler, Nova Sonic WebSocket
// bridge, and Telnyx Call Control handler based on config availability.
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
		// Build Stripe checkout service for voice deposits
		var voiceCheckoutSvc voice.CheckoutLinkCreator
		if cfg.StripeSecretKey != "" {
			stripeSvc := payments.NewStripeCheckoutService(cfg.StripeSecretKey, cfg.StripeSuccessURL, cfg.StripeCancelURL, logger)
			if clinicStore != nil {
				voiceCheckoutSvc = payments.NewMultiCheckoutService(nil, stripeSvc, clinicStore, logger)
			} else {
				voiceCheckoutSvc = stripeSvc
			}
			logger.Info("voice: Stripe checkout service configured for deposits")
		} else {
			logger.Warn("voice: no STRIPE_SECRET_KEY — deposits will use fallback test link")
		}

		voiceToolDeps := &voice.ToolDeps{
			MoxieClient: func() *moxieclient.Client {
				moxieDryRun := os.Getenv("MOXIE_DRY_RUN") == "true"
				return moxieclient.NewClient(logger, moxieclient.WithDryRun(moxieDryRun))
			}(),
			Messenger:       webhookMessenger,
			ClinicStore:     clinicStore,
			LeadsRepo:       leadsRepo,
			CheckoutService: voiceCheckoutSvc,
		}

		voiceWSHandler = voice.NewTelnyxWSHandler(slog.Default(), func(l *slog.Logger, callControlID string, mediaFormat voice.TelnyxMediaFormat, callCtx voice.CallContext) (*voice.Bridge, error) {
			systemPrompt := voice.BuildVoiceSystemPrompt(l, voiceToolDeps.ClinicStore, callCtx.OrgID)

			// Pre-fetch Boulevard availability so Lauren has real slots
			// (Nova Sonic tool calling is disabled due to audio incompatibility)
			if voiceToolDeps.ClinicStore != nil {
				if cfg, err := voiceToolDeps.ClinicStore.Get(context.Background(), callCtx.OrgID); err == nil && cfg != nil && cfg.UsesBoulevardBooking() {
					if blvdClient := boulevard.NewBoulevardClient(cfg.BoulevardBusinessID, cfg.BoulevardLocationID, logger); blvdClient != nil {
						adapter := boulevard.NewBoulevardAdapter(blvdClient, os.Getenv("MOXIE_DRY_RUN") == "true", logger)
						fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 8*time.Second)
						// Fetch for common services to get broad availability
						// Empty string matches first service alphabetically which may have limited days
						// "Botox" is the most commonly requested service
						slots, _, blvdErr := adapter.ResolveAvailabilityWithCart(fetchCtx, "Botox", "", time.Now())
						fetchCancel()
						if blvdErr == nil && len(slots) > 0 {
							// Filter slots against business hours
							getDayHours := func(bh clinic.BusinessHours, day time.Weekday) *clinic.DayHours {
								switch day {
								case time.Monday:
									return bh.Monday
								case time.Tuesday:
									return bh.Tuesday
								case time.Wednesday:
									return bh.Wednesday
								case time.Thursday:
									return bh.Thursday
								case time.Friday:
									return bh.Friday
								case time.Saturday:
									return bh.Saturday
								case time.Sunday:
									return bh.Sunday
								}
								return nil
							}
							var validSlots []boulevard.TimeSlot
							for _, s := range slots {
								dh := getDayHours(cfg.BusinessHours, s.StartAt.Weekday())
								if dh == nil {
									// Day not configured = clinic closed that day, skip
									continue
								}
								validSlots = append(validSlots, s)
							}
							var slotLines []string
							for i, s := range validSlots {
								if i >= 20 {
									break
								}
								slotLines = append(slotLines, fmt.Sprintf("- %s", s.StartAt.Format("Mon Jan 2 at 3:04 PM")))
							}
							if len(slotLines) > 0 {
								systemPrompt += "\n\n=== AVAILABLE APPOINTMENT TIMES (FROM BOOKING SYSTEM) ===\n" +
									"THESE ARE THE ONLY TIMES THAT EXIST. There are NO other times.\n" +
									"You MUST pick from this list. Any time not on this list DOES NOT EXIST.\n\n" +
									strings.Join(slotLines, "\n") +
									"\n\n=== END OF AVAILABLE TIMES ===\n" +
									"STRICT RULE: If the caller wants times not on this list, say: " +
									"\"I don't have openings matching that right now. Would you like to hear what I do have available?\" " +
									"Then read times from the list above. NEVER say a time that is not on the list."
							}
							logger.Info("voice: pre-fetched Boulevard availability", "raw_slots", len(slots), "valid_slots", len(validSlots), "org_id", callCtx.OrgID, "times", strings.Join(slotLines, "; "))
						} else if blvdErr != nil {
							logger.Warn("voice: Boulevard prefetch failed", "error", blvdErr, "org_id", callCtx.OrgID)
						}
					}
				}
			}

			// Resolve clinic name for ElevenLabs greeting
			clinicName := "our office"
			if voiceToolDeps.ClinicStore != nil {
				if cfg, err := voiceToolDeps.ClinicStore.Get(context.Background(), callCtx.OrgID); err == nil && cfg != nil && cfg.Name != "" {
					clinicName = cfg.Name
				}
			}

			// Skip sidecar ElevenLabs greeting — Telnyx TTS fires instantly on call.answered
			// for sub-second greeting latency. Use "__SKIP__" sentinel to disable sidecar greeting.
			voiceGreeting := "__SKIP__"

			conversationID := "voice:" + callCtx.OrgID + ":" + strings.TrimPrefix(callCtx.From, "+")
			onTranscript := func(role, text string) {
				if conversationStore == nil || strings.TrimSpace(text) == "" {
					return
				}
				now := time.Now()
				from := callCtx.From
				to := callCtx.To
				status := "delivered"
				if role == "assistant" {
					from = callCtx.To
					to = callCtx.From
					status = "sent"
				}
				if err := conversationStore.AppendMessage(context.Background(), conversationID, conversation.SMSTranscriptMessage{
					Role:      role,
					From:      from,
					To:        to,
					Body:      text,
					Timestamp: now,
					Status:    status,
					Metadata:  map[string]string{"channel": "voice", "source": "nova-bridge"},
				}); err != nil {
					logger.Error("voice bridge: failed to persist transcript", "error", err, "conversation_id", conversationID)
				}
			}

			return voice.NewBridge(
				context.Background(),
				voice.BridgeConfig{
					SidecarURL:     sidecarURL,
					SystemPrompt:   systemPrompt,
					Voice:          novaSonicVoice,
					OrgID:          callCtx.OrgID,
					CallerPhone:    callCtx.From,
					CalledPhone:    callCtx.To,
					ClinicName:     clinicName,
					Greeting:       voiceGreeting,
					ToolDeps:       voiceToolDeps,
					Redis:          redisClient,
					OnTranscript:   onTranscript,
					ConversationID: conversationID,
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
		// Post-call SMS service: sends deposit link after voice call hangup
		// (workaround for Nova Sonic tools being disabled).
		postCallSMS := voice.NewPostCallSMSService(voice.PostCallSMSConfig{
			Logger:      slog.Default(),
			Messenger:   webhookMessenger,
			OrgResolver: resolver,
			ClinicStore: clinicStore,
		})

		callControlHandler = handlers.NewCallControlHandler(handlers.CallControlConfig{
			Logger:       logger,
			TelnyxAPIKey: cfg.TelnyxAPIKey,
			StreamURL:    cfg.NovaSonicStreamURL,
			OrgResolver:  resolver,
			ClinicStore:  clinicStore,
			PostCallSMS:  postCallSMS,
		})
		logger.Info("call control handler initialized", "stream_url", cfg.NovaSonicStreamURL)
	}

	return VoiceBootstrap{VoiceAIHandler: voiceAIHandler, VoiceWSHandler: voiceWSHandler, CallControl: callControlHandler}
}

// BootstrapNotifications creates the GitHub webhook handler that forwards
// CI/CD events to Telegram. Returns nil if GITHUB_WEBHOOK_SECRET is not set.
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
