package router

import (
	"github.com/go-chi/chi/v5"
	httpmiddleware "github.com/wolfman30/medspa-ai-platform/internal/http/middleware"
)

// registerTenantRoutes mounts tenant-scoped API routes. These are rate-limited
// and require an X-Org-ID header to scope requests to a specific clinic.
func registerTenantRoutes(r chi.Router, cfg *Config) {
	r.Group(func(tenant chi.Router) {
		tenant.Use(httpmiddleware.RateLimit(50, 100))
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
				r.Use(requireOnboardingToken(cfg.OnboardingToken))
				r.Post("/{clinicID}", cfg.ConversationHandler.AddKnowledge)
			})
		}
	})
}
