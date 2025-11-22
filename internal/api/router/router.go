package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	httpmiddleware "github.com/wolfman30/medspa-ai-platform/internal/http/middleware"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Config holds router configuration
type Config struct {
	Logger              *logging.Logger
	LeadsHandler        *leads.Handler
	MessagingHandler    *messaging.Handler
	ConversationHandler *conversation.Handler
	PaymentsHandler     *payments.CheckoutHandler
	SquareWebhook       *payments.SquareWebhookHandler
	AdminMessaging      *handlers.AdminMessagingHandler
	TelnyxWebhooks      *handlers.TelnyxWebhookHandler
	AdminAuthSecret     string
	MetricsHandler      http.Handler
}

// New creates a new Chi router with all routes configured
func New(cfg *Config) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	if cfg.Logger != nil {
		r.Use(httpmiddleware.RequestLogger(cfg.Logger))
	}

	// Public endpoints (webhooks, health checks)
	r.Group(func(public chi.Router) {
		public.Get("/health", cfg.MessagingHandler.HealthCheck)
		public.Route("/messaging", func(r chi.Router) {
			r.Post("/twilio/webhook", cfg.MessagingHandler.TwilioWebhook)
		})
		public.Route("/webhooks/twilio", func(r chi.Router) {
			r.Post("/voice", cfg.MessagingHandler.TwilioVoiceWebhook)
		})
		if cfg.SquareWebhook != nil {
			public.Post("/webhooks/square", cfg.SquareWebhook.Handle)
		}
		if cfg.TelnyxWebhooks != nil {
			public.Post("/webhooks/telnyx/messages", cfg.TelnyxWebhooks.HandleMessages)
			public.Post("/webhooks/telnyx/hosted", cfg.TelnyxWebhooks.HandleHosted)
		}
		if cfg.MetricsHandler != nil {
			public.Handle("/metrics", cfg.MetricsHandler)
		}
	})

	if cfg.AdminMessaging != nil && cfg.AdminAuthSecret != "" {
		r.Route("/admin", func(admin chi.Router) {
			admin.Use(httpmiddleware.AdminJWT(cfg.AdminAuthSecret))
			admin.Post("/hosted/orders", cfg.AdminMessaging.StartHostedOrder)
			admin.Post("/10dlc/brands", cfg.AdminMessaging.CreateBrand)
			admin.Post("/10dlc/campaigns", cfg.AdminMessaging.CreateCampaign)
			admin.Post("/messages:send", cfg.AdminMessaging.SendMessage)
		})
	}

	// Tenant-scoped API routes
	r.Group(func(tenant chi.Router) {
		tenant.Use(requireOrgID)

		tenant.Route("/leads", func(r chi.Router) {
			r.Post("/web", cfg.LeadsHandler.CreateWebLead)
		})

		if cfg.PaymentsHandler != nil {
			tenant.Route("/payments", func(r chi.Router) {
				r.Post("/checkout", cfg.PaymentsHandler.CreateCheckout)
			})
		}

		if cfg.ConversationHandler != nil {
			tenant.Route("/conversations", func(r chi.Router) {
				r.Post("/start", cfg.ConversationHandler.Start)
				r.Post("/message", cfg.ConversationHandler.Message)
				r.Get("/jobs/{jobID}", cfg.ConversationHandler.JobStatus)
			})
			tenant.Route("/knowledge", func(r chi.Router) {
				r.Post("/{clinicID}", cfg.ConversationHandler.AddKnowledge)
			})
		}
	})

	return r
}
