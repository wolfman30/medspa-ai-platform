package router

import (
	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	httpmiddleware "github.com/wolfman30/medspa-ai-platform/internal/http/middleware"
)

// registerPortalRoutes mounts customer-facing portal endpoints. These are
// read-only views scoped to the authenticated org owner's clinic.
func registerPortalRoutes(r chi.Router, cfg *Config) {
	if cfg.DB == nil {
		return
	}
	if cfg.AdminAuthSecret == "" && cfg.CognitoUserPoolID == "" {
		return
	}

	r.Route("/portal", func(portal chi.Router) {
		cognitoCfg := httpmiddleware.CognitoConfig{
			Region:     cfg.CognitoRegion,
			UserPoolID: cfg.CognitoUserPoolID,
			ClientID:   cfg.CognitoClientID,
		}
		portal.Use(httpmiddleware.CognitoOrAdminJWT(cognitoCfg, cfg.AdminAuthSecret))

		dashboardHandler := handlers.NewPortalDashboardHandler(cfg.DB, cfg.Logger)
		conversationsHandler := handlers.NewAdminConversationsHandler(cfg.DB, cfg.TranscriptStore, cfg.Logger)
		depositsHandler := handlers.NewAdminDepositsHandler(cfg.DB, cfg.Logger)
		var knowledgeHandler *handlers.PortalKnowledgeHandler
		if cfg.KnowledgeRepo != nil {
			knowledgeHandler = handlers.NewPortalKnowledgeHandler(cfg.KnowledgeRepo, cfg.AuditService, cfg.Logger)
		}

		portal.Route("/orgs/{orgID}", func(r chi.Router) {
			r.Use(requirePortalOrgOwner(cfg.DB, cfg.Logger))
			r.Get("/", dashboardHandler.IndexPage)
			r.Get("/dashboard", dashboardHandler.GetDashboard)
			r.Get("/conversations", conversationsHandler.ListConversations)
			r.Get("/conversations/{conversationID}", conversationsHandler.GetConversation)
			r.Get("/deposits", depositsHandler.ListDeposits)
			r.Get("/deposits/stats", depositsHandler.GetDepositStats)
			r.Get("/deposits/{depositID}", depositsHandler.GetDeposit)
			if cfg.SquareOAuth != nil {
				r.Get("/square/status", cfg.SquareOAuth.HandleStatus)
				r.Get("/square/connect", cfg.SquareOAuth.HandleConnect)
			}
			if cfg.StripeConnect != nil {
				r.Get("/stripe/status", cfg.StripeConnect.HandleStatus)
				r.Get("/stripe/connect", cfg.StripeConnect.HandleAuthorize)
			}
			if knowledgeHandler != nil {
				r.Get("/knowledge", knowledgeHandler.GetKnowledge)
				r.Put("/knowledge", knowledgeHandler.PutKnowledge)
				r.Get("/knowledge/page", knowledgeHandler.KnowledgePage)
			}
			if cfg.StructuredKnowledgeHandler != nil {
				r.Get("/knowledge/structured", cfg.StructuredKnowledgeHandler.GetStructuredKnowledge)
				r.Put("/knowledge/structured", cfg.StructuredKnowledgeHandler.PutStructuredKnowledge)
				r.Post("/knowledge/sync-moxie", cfg.StructuredKnowledgeHandler.SyncMoxie)
			}
		})
	})
}
