package router

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/wolfman30/medspa-ai-platform/internal/channels/instagram"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/http/handlers"
	httpmiddleware "github.com/wolfman30/medspa-ai-platform/internal/http/middleware"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/payments"
	"github.com/wolfman30/medspa-ai-platform/internal/prospects"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Config holds router configuration
type Config struct {
	Logger              *logging.Logger
	LeadsHandler        *leads.Handler
	MessagingHandler    *messaging.Handler
	ConversationHandler *conversation.Handler
	PaymentsHandler     *payments.CheckoutHandler
	FakePayments        *payments.FakePaymentsHandler
	SquareWebhook       *payments.SquareWebhookHandler
	SquareOAuth         *payments.OAuthHandler
	AdminMessaging      *handlers.AdminMessagingHandler
	AdminClinicData     *handlers.AdminClinicDataHandler
	TelnyxWebhooks      *handlers.TelnyxWebhookHandler
	ClinicHandler       *clinic.Handler
	ClinicStatsHandler  *clinic.StatsHandler
	ClinicDashboard     *clinic.DashboardHandler
	AdminOnboarding     *handlers.AdminOnboardingHandler
	OnboardingToken     string
	AdminAuthSecret     string
	MetricsHandler      http.Handler
	CORSAllowedOrigins  []string

	// Cognito auth config (optional, enables Cognito JWT validation)
	CognitoUserPoolID string
	CognitoClientID   string
	CognitoRegion     string

	// Admin dashboard dependencies (optional)
	DB              *sql.DB
	TranscriptStore *conversation.SMSTranscriptStore
	ClinicStore     *clinic.Store
	KnowledgeRepo   conversation.KnowledgeRepository
	AuditService    *compliance.AuditService

	// Client self-service registration
	ClientRegistration *handlers.ClientRegistrationHandler

	// Stripe payment handlers
	StripeWebhook *payments.StripeWebhookHandler
	StripeConnect *payments.StripeConnectHandler

	// Structured knowledge handler
	StructuredKnowledgeHandler *handlers.StructuredKnowledgeHandler

	// Booking callback handler (browser sidecar → Go API)
	BookingCallbackHandler *conversation.BookingCallbackHandler

	// Short payment URL redirect handler
	PaymentRedirect *payments.RedirectHandler

	// Morning briefs handler
	AdminBriefs *handlers.AdminBriefsHandler

	// Prospect tracker
	ProspectsHandler *prospects.Handler

	// Voice AI handler (Telnyx AI Assistant webhook)
	VoiceAIHandler *handlers.VoiceAIHandler

	// GitHub Actions webhook handler
	GitHubWebhook *handlers.GitHubWebhookHandler

	// Instagram DM adapter
	InstagramAdapter *instagram.Adapter

	// Evidence upload S3
	EvidenceS3Client handlers.S3Uploader
	EvidenceS3Bucket string
	EvidenceS3Region string

	// Readiness check dependencies
	RedisClient    *redis.Client
	HasSMSProvider bool
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
	if len(cfg.CORSAllowedOrigins) > 0 {
		r.Use(httpmiddleware.CORS(cfg.CORSAllowedOrigins))
	}
	if cfg.Logger != nil {
		r.Use(httpmiddleware.RequestLogger(cfg.Logger))
	}

	// Public endpoints (webhooks, health checks)
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

	// Admin routes (protected by JWT - supports both legacy HMAC and Cognito RS256)
	if cfg.AdminAuthSecret != "" || cfg.CognitoUserPoolID != "" {
		r.Route("/admin", func(admin chi.Router) {
			cognitoCfg := httpmiddleware.CognitoConfig{
				Region:     cfg.CognitoRegion,
				UserPoolID: cfg.CognitoUserPoolID,
				ClientID:   cfg.CognitoClientID,
			}
			admin.Use(httpmiddleware.CognitoOrAdminJWT(cognitoCfg, cfg.AdminAuthSecret))
			if cfg.ConversationHandler != nil {
			}
			if cfg.AdminMessaging != nil {
				admin.Post("/hosted/orders", cfg.AdminMessaging.StartHostedOrder)
				admin.Post("/hosted/activate", cfg.AdminMessaging.ActivateHostedNumber)
				admin.Post("/10dlc/brands", cfg.AdminMessaging.CreateBrand)
				admin.Post("/10dlc/campaigns", cfg.AdminMessaging.CreateCampaign)
				admin.Post("/messages:send", cfg.AdminMessaging.SendMessage)
			}
			// Morning briefs
			if cfg.AdminBriefs != nil {
				admin.Get("/briefs", cfg.AdminBriefs.ListBriefs)
				admin.Get("/briefs/{date}", cfg.AdminBriefs.GetBrief)
				admin.Post("/briefs", cfg.AdminBriefs.CreateBrief)
				admin.Put("/briefs/seed", cfg.AdminBriefs.SeedBriefs)
			}
			// Prospect tracker
			if cfg.ProspectsHandler != nil {
				admin.Get("/prospects", cfg.ProspectsHandler.List)
				admin.Get("/prospects/{prospectID}", cfg.ProspectsHandler.Get)
				admin.Put("/prospects/{prospectID}", cfg.ProspectsHandler.Upsert)
				admin.Delete("/prospects/{prospectID}", cfg.ProspectsHandler.Delete)
				admin.Post("/prospects/{prospectID}/events", cfg.ProspectsHandler.AddEvent)
				admin.Get("/prospects/{prospectID}/outreach", cfg.ProspectsHandler.GetOutreach)
				admin.Get("/rule100/today", cfg.ProspectsHandler.GetRule100Today)
			}
			// Clinic onboarding endpoints
			if cfg.AdminOnboarding != nil {
				admin.Post("/clinics", cfg.AdminOnboarding.CreateClinic)
				admin.Post("/onboarding/prefill", cfg.AdminOnboarding.PrefillFromWebsite)
			}
			// Clinic routes (config + Square OAuth)
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

			// Admin dashboard, leads, and conversations routes
			if cfg.DB != nil {
				handlers.RegisterAdminRoutes(admin, cfg.DB, cfg.TranscriptStore, cfg.ClinicStore, cfg.Logger)

				// Manual testing tracker
				testingHandler := handlers.NewAdminTestingHandler(cfg.DB, cfg.Logger, cfg.EvidenceS3Client, cfg.EvidenceS3Bucket, cfg.EvidenceS3Region)
				admin.Get("/testing", testingHandler.ListTestResults)
				admin.Post("/testing", testingHandler.CreateTestResult)
				admin.Put("/testing/{id}", testingHandler.UpdateTestResult)
				admin.Post("/testing/{id}/evidence", testingHandler.UploadEvidence)
				admin.Delete("/testing/{id}/evidence", testingHandler.DeleteEvidence)
			}
		})
	}

	// Customer portal routes (read-only, scoped to org owner)
	if cfg.DB != nil && (cfg.AdminAuthSecret != "" || cfg.CognitoUserPoolID != "") {
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
				r.Use(requireOnboardingToken(cfg.OnboardingToken))
				r.Post("/{clinicID}", cfg.ConversationHandler.AddKnowledge)
			})
		}
	})

	return r
}

// readinessHandler returns 200 only when critical services are connected.
func readinessHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{}
		ready := true

		// Database check
		if cfg.DB != nil {
			if err := cfg.DB.PingContext(r.Context()); err != nil {
				checks["database"] = "unhealthy: " + err.Error()
				ready = false
			} else {
				checks["database"] = "ok"
			}
		} else {
			checks["database"] = "not configured"
		}

		// Redis check
		if cfg.RedisClient != nil {
			if err := cfg.RedisClient.Ping(r.Context()).Err(); err != nil {
				checks["redis"] = "unhealthy: " + err.Error()
				ready = false
			} else {
				checks["redis"] = "ok"
			}
		} else {
			checks["redis"] = "not configured"
		}

		// SMS provider check
		if cfg.HasSMSProvider {
			checks["sms"] = "ok"
		} else {
			checks["sms"] = "no provider configured"
			ready = false
		}

		w.Header().Set("Content-Type", "application/json")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		resp := map[string]interface{}{"ready": ready, "checks": checks}
		json.NewEncoder(w).Encode(resp)
	}
}
