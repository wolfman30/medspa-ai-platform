package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	httpmiddleware "github.com/wolfman30/medspa-ai-platform/internal/http/middleware"
)

// adminAuthMiddleware returns the shared JWT auth middleware used by all admin routes.
func adminAuthMiddleware(cfg *Config) func(http.Handler) http.Handler {
	return httpmiddleware.CognitoOrAdminJWT(
		httpmiddleware.CognitoConfig{
			Region:     cfg.CognitoRegion,
			UserPoolID: cfg.CognitoUserPoolID,
			ClientID:   cfg.CognitoClientID,
		},
		cfg.AdminAuthSecret,
	)
}

// registerAdminRoutes mounts all admin-only endpoints behind JWT authentication.
// Supports both legacy HMAC and Cognito RS256 tokens.
func registerAdminRoutes(r chi.Router, cfg *Config) {
	if cfg.AdminAuthSecret == "" && cfg.CognitoUserPoolID == "" {
		return
	}

	authMW := adminAuthMiddleware(cfg)

	// Revenue attribution dashboard endpoint requested by CEO dashboard.
	if cfg.DB != nil {
		revenueHandler := handlers.NewRevenueDashboardHandler(cfg.DB, cfg.ClinicStore, cfg.Logger)
		r.With(authMW).Get("/api/dashboard/revenue", revenueHandler.GetRevenueDashboard)
	}

	r.Route("/admin", func(admin chi.Router) {
		admin.Use(authMW)
		if cfg.ConversationHandler != nil {
		}
		if cfg.AdminMessaging != nil {
			admin.Post("/hosted/orders", cfg.AdminMessaging.StartHostedOrder)
			admin.Post("/hosted/activate", cfg.AdminMessaging.ActivateHostedNumber)
			admin.Post("/hosted/deactivate", cfg.AdminMessaging.DeactivateHostedNumber)
			admin.Post("/10dlc/brands", cfg.AdminMessaging.CreateBrand)
			admin.Post("/10dlc/campaigns", cfg.AdminMessaging.CreateCampaign)
			admin.Post("/messages:send", cfg.AdminMessaging.SendMessage)
		}
		// Agent team status
		admin.Get("/agents/status", handlers.HandleAgentsStatus)

		registerAdminBriefsRoutes(admin, cfg)
		registerAdminFinanceRoutes(admin, cfg)
		registerAdminProspectsRoutes(admin, cfg)
		registerAdminStoriesRoutes(admin, cfg)
		registerAdminOnboardingRoutes(admin, cfg)
		registerAdminClinicRoutes(admin, cfg)
		registerAdminDashboardRoutes(admin, cfg)
	})
}

// registerAdminBriefsRoutes mounts the morning briefs CRUD endpoints.
func registerAdminBriefsRoutes(admin chi.Router, cfg *Config) {
	if cfg.AdminBriefs == nil {
		return
	}
	admin.Get("/briefs", cfg.AdminBriefs.ListBriefs)
	admin.Get("/briefs/{date}", cfg.AdminBriefs.GetBrief)
	admin.Post("/briefs", cfg.AdminBriefs.CreateBrief)
	admin.Put("/briefs/seed", cfg.AdminBriefs.SeedBriefs)
}

// registerAdminFinanceRoutes mounts the Plaid-backed finance dashboard
// and research intelligence endpoints.
func registerAdminFinanceRoutes(admin chi.Router, cfg *Config) {
	if cfg.AdminFinance == nil {
		return
	}
	admin.Get("/finance/balances", cfg.AdminFinance.GetBalances)
	admin.Get("/finance/transactions", cfg.AdminFinance.GetTransactions)
	admin.Get("/finance/budget", cfg.AdminFinance.GetBudget)
	admin.Put("/finance/budget", cfg.AdminFinance.PutBudget)

	if cfg.AdminResearch != nil {
		admin.Get("/research", cfg.AdminResearch.ListDocs)
		admin.Put("/research", cfg.AdminResearch.PutDoc)
	}
}

// registerAdminProspectsRoutes mounts the prospect tracker CRUD endpoints.
func registerAdminProspectsRoutes(admin chi.Router, cfg *Config) {
	if cfg.ProspectsHandler == nil {
		return
	}
	admin.Get("/prospects", cfg.ProspectsHandler.List)
	admin.Post("/prospects", cfg.ProspectsHandler.Create)
	admin.Get("/prospects/{prospectID}", cfg.ProspectsHandler.Get)
	admin.Put("/prospects/{prospectID}", cfg.ProspectsHandler.Upsert)
	admin.Delete("/prospects/{prospectID}", cfg.ProspectsHandler.Delete)
	admin.Post("/prospects/{prospectID}/events", cfg.ProspectsHandler.AddEvent)
	admin.Get("/prospects/{prospectID}/outreach", cfg.ProspectsHandler.GetOutreach)
	admin.Get("/rule100/today", cfg.ProspectsHandler.GetRule100Today)
}

// registerAdminStoriesRoutes mounts the Kanban stories board CRUD endpoints.
func registerAdminStoriesRoutes(admin chi.Router, cfg *Config) {
	if cfg.StoriesHandler == nil {
		return
	}
	admin.Get("/stories", cfg.StoriesHandler.List)
	admin.Post("/stories", cfg.StoriesHandler.Create)
	admin.Get("/stories/{id}", cfg.StoriesHandler.Get)
	admin.Put("/stories/{id}", cfg.StoriesHandler.Update)
	admin.Delete("/stories/{id}", cfg.StoriesHandler.Delete)
}

// registerAdminOnboardingRoutes mounts admin-only clinic onboarding endpoints.
func registerAdminOnboardingRoutes(admin chi.Router, cfg *Config) {
	if cfg.AdminOnboarding == nil {
		return
	}
	admin.Post("/clinics", cfg.AdminOnboarding.CreateClinic)
	admin.Post("/onboarding/prefill", cfg.AdminOnboarding.PrefillFromWebsite)
}

// registerAdminClinicRoutes mounts per-clinic admin endpoints for config,
// knowledge, stats, conversations, data management, and payment integrations.
func registerAdminClinicRoutes(admin chi.Router, cfg *Config) {
	admin.Route("/clinics/{orgID}", func(clinicRoutes chi.Router) {
		if cfg.AdminOnboarding != nil {
			clinicRoutes.Get("/onboarding-status", cfg.AdminOnboarding.GetOnboardingStatus)
		}
		if cfg.ClinicHandler != nil {
			clinicRoutes.Get("/config", cfg.ClinicHandler.GetConfig)
			clinicRoutes.Put("/config", cfg.ClinicHandler.UpdateConfig)
			clinicRoutes.Post("/config", cfg.ClinicHandler.UpdateConfig)
		}
		if cfg.KnowledgeRepo != nil {
			knowledgeHandler := handlers.NewPortalKnowledgeHandler(cfg.KnowledgeRepo, cfg.AuditService, cfg.Logger)
			clinicRoutes.Get("/knowledge", knowledgeHandler.GetKnowledge)
			clinicRoutes.Put("/knowledge", knowledgeHandler.PutKnowledge)
		}
		if cfg.StructuredKnowledgeHandler != nil {
			clinicRoutes.Get("/knowledge/structured", cfg.StructuredKnowledgeHandler.GetStructuredKnowledge)
			clinicRoutes.Put("/knowledge/structured", cfg.StructuredKnowledgeHandler.PutStructuredKnowledge)
			clinicRoutes.Post("/knowledge/sync-moxie", cfg.StructuredKnowledgeHandler.SyncMoxie)
		}
		if cfg.ClinicStatsHandler != nil {
			clinicRoutes.Get("/stats", cfg.ClinicStatsHandler.GetStats)
		}
		if cfg.ClinicDashboard != nil {
			clinicRoutes.Get("/dashboard", cfg.ClinicDashboard.GetDashboard)
		}
		if cfg.LeadsHandler != nil {
			clinicRoutes.Get("/leads", cfg.LeadsHandler.ListLeads)
		}
		if cfg.ConversationHandler != nil {
			clinicRoutes.Get("/conversations/{phone}", cfg.ConversationHandler.GetTranscript)
			clinicRoutes.Get("/sms/{phone}", cfg.ConversationHandler.GetSMSTranscript)
		}
		if cfg.AdminClinicData != nil {
			clinicRoutes.Delete("/phones/{phone}", cfg.AdminClinicData.PurgePhone)
			clinicRoutes.Delete("/data", cfg.AdminClinicData.PurgeOrg)
		}
		if cfg.SquareOAuth != nil {
			clinicRoutes.Get("/square/connect", cfg.SquareOAuth.HandleConnect)
			clinicRoutes.Get("/square/status", cfg.SquareOAuth.HandleStatus)
			clinicRoutes.Delete("/square/disconnect", cfg.SquareOAuth.HandleDisconnect)
			clinicRoutes.Post("/square/sync-location", cfg.SquareOAuth.HandleSyncLocation)
			clinicRoutes.Post("/square/setup", cfg.SquareOAuth.HandleSandboxSetup)
			clinicRoutes.Put("/phone", cfg.SquareOAuth.HandleUpdatePhone)
		}
		if cfg.StripeConnect != nil {
			clinicRoutes.Get("/stripe/connect", cfg.StripeConnect.HandleAuthorize)
			clinicRoutes.Get("/stripe/status", cfg.StripeConnect.HandleStatus)
		}
	})
}

// registerAdminDashboardRoutes mounts admin dashboard, leads, conversations,
// and manual testing tracker endpoints.
func registerAdminDashboardRoutes(admin chi.Router, cfg *Config) {
	if cfg.DB == nil {
		return
	}
	handlers.RegisterAdminRoutes(admin, cfg.DB, cfg.TranscriptStore, cfg.ClinicStore, cfg.Logger)

	testingHandler := handlers.NewAdminTestingHandler(cfg.DB, cfg.Logger, cfg.EvidenceS3Client, cfg.EvidenceS3Bucket, cfg.EvidenceS3Region)
	admin.Get("/testing", testingHandler.ListTestResults)
	admin.Post("/testing", testingHandler.CreateTestResult)
	admin.Put("/testing/{id}", testingHandler.UpdateTestResult)
	admin.Post("/testing/{id}/evidence", testingHandler.UploadEvidence)
	admin.Delete("/testing/{id}/evidence", testingHandler.DeleteEvidence)
}
