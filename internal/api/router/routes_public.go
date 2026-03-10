package router

import (
	"github.com/go-chi/chi/v5"
	httpmiddleware "github.com/wolfman30/medspa-ai-platform/internal/http/middleware"
)

// registerPublicRoutes mounts unauthenticated endpoints: webhooks, health
// checks, payment callbacks, OAuth flows, and self-service onboarding.
func registerPublicRoutes(r chi.Router, cfg *Config) {
	r.Group(func(public chi.Router) {
		public.Get("/health", cfg.MessagingHandler.HealthCheck)
		public.Get("/ready", readinessHandler(cfg))
		public.Route("/messaging", func(r chi.Router) {
			r.Use(httpmiddleware.RateLimit(100, 200))
			r.Post("/twilio/webhook", cfg.MessagingHandler.TwilioWebhook)
		})
		public.Route("/webhooks/twilio", func(r chi.Router) {
			r.Use(httpmiddleware.RateLimit(100, 200))
			r.Post("/voice", cfg.MessagingHandler.TwilioVoiceWebhook)
		})
		if cfg.SquareWebhook != nil {
			public.Post("/webhooks/square", cfg.SquareWebhook.Handle)
		}
		if cfg.StripeWebhook != nil {
			public.Post("/webhooks/stripe", cfg.StripeWebhook.Handle)
		}
		if cfg.Billing != nil {
			public.Post("/api/subscribe", cfg.Billing.HandleSubscribe)
		}
		if cfg.BillingWebhook != nil {
			public.Post("/webhooks/stripe-billing", cfg.BillingWebhook.Handle)
		}
		if cfg.StripeConnect != nil {
			public.Get("/stripe/connect/authorize", cfg.StripeConnect.HandleAuthorize)
			public.Get("/stripe/connect/callback", cfg.StripeConnect.HandleCallback)
		}
		if cfg.FakePayments != nil {
			public.Mount("/demo", cfg.FakePayments.Routes())
		}
		if cfg.TelnyxWebhooks != nil {
			public.Post("/webhooks/telnyx/messages", cfg.TelnyxWebhooks.HandleMessages)
			public.Post("/webhooks/telnyx/hosted", cfg.TelnyxWebhooks.HandleHosted)
			public.Post("/webhooks/telnyx/voice", cfg.TelnyxWebhooks.HandleVoice)
		}
		if cfg.VoiceAIHandler != nil {
			public.Post("/webhooks/telnyx/voice-ai", cfg.VoiceAIHandler.HandleVoiceAI)
		}
		if cfg.CallControlHandler != nil {
			public.Post("/webhooks/telnyx/call-control", cfg.CallControlHandler.HandleCallControl)
		}
		if cfg.InstagramAdapter != nil {
			public.Get("/webhooks/instagram", cfg.InstagramAdapter.HandleVerification)
			public.Post("/webhooks/instagram", cfg.InstagramAdapter.HandleWebhook)
		}
		if cfg.GitHubWebhook != nil {
			public.Post("/webhooks/github", cfg.GitHubWebhook.Handle)
		}
		if cfg.BookingCallbackHandler != nil {
			public.Post("/webhooks/booking/callback", cfg.BookingCallbackHandler.Handle)
		}
		if cfg.VoiceWSHandler != nil {
			public.Get("/ws/voice", cfg.VoiceWSHandler.ServeHTTP)
		}
		if cfg.WebChatHandler != nil {
			public.Get("/chat/ws", cfg.WebChatHandler.HandleWebSocket)
			public.Post("/chat/message", cfg.WebChatHandler.HandleMessage)
			public.Get("/chat/history", cfg.WebChatHandler.HandleHistory)
			public.Get("/chat/widget.js", cfg.WebChatHandler.HandleWidgetJS)
		}
		if cfg.PaymentRedirect != nil {
			public.Get("/pay/{code}", cfg.PaymentRedirect.Handle)
		}
		if cfg.MetricsHandler != nil {
			public.Handle("/metrics", cfg.MetricsHandler)
		}
		// OAuth callback (public, no auth required)
		if cfg.SquareOAuth != nil {
			public.Mount("/oauth", cfg.SquareOAuth.Routes())
		}
		// DEV ONLY: Public phone activation (bypasses auth for development)
		if cfg.AdminMessaging != nil {
			public.With(requireOnboardingToken(cfg.OnboardingToken)).Post("/dev/activate-phone", cfg.AdminMessaging.ActivateHostedNumber)
		}
		// Client self-service registration (public - called after Cognito signup)
		if cfg.ClientRegistration != nil {
			public.Route("/api/client", func(r chi.Router) {
				r.Post("/register", cfg.ClientRegistration.RegisterClinic)
				r.Get("/org", cfg.ClientRegistration.LookupOrgByEmail)
			})
		}
		// Public onboarding routes (self-service)
		if cfg.AdminOnboarding != nil {
			public.Route("/onboarding", func(r chi.Router) {
				r.Use(requireOnboardingToken(cfg.OnboardingToken))
				r.Post("/prefill", cfg.AdminOnboarding.PrefillFromWebsite)
				r.Post("/clinics", cfg.AdminOnboarding.CreateClinic)
				r.Route("/clinics/{orgID}", func(clinic chi.Router) {
					clinic.Get("/status", cfg.AdminOnboarding.GetOnboardingStatus)
					if cfg.ClinicHandler != nil {
						clinic.Get("/config", cfg.ClinicHandler.GetConfig)
						clinic.Put("/config", cfg.ClinicHandler.UpdateConfig)
					}
					// Square OAuth for onboarding (public)
					if cfg.SquareOAuth != nil {
						clinic.Get("/square/connect", cfg.SquareOAuth.HandleConnect)
						clinic.Get("/square/status", cfg.SquareOAuth.HandleStatus)
					}
					// Stripe Connect for onboarding (public)
					if cfg.StripeConnect != nil {
						clinic.Get("/stripe/connect", cfg.StripeConnect.HandleAuthorize)
						clinic.Get("/stripe/status", cfg.StripeConnect.HandleStatus)
					}
				})
			})
		}
	})
}
