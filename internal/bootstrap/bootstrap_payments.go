package bootstrap

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	appconfig "github.com/wolfman30/medspa-ai-platform/internal/config"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

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
