package router

import "github.com/go-chi/chi/v5"

// registerTenantRoutes mounts org-scoped API endpoints for leads, payments,
// conversations, and knowledge ingestion.
func registerTenantRoutes(r chi.Router, cfg *Config) {
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
				r.Use(requireOnboardingToken(cfg.OnboardingToken))
				r.Post("/{clinicID}", cfg.ConversationHandler.AddKnowledge)
			})
		}
	})
}
