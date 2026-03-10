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
	"github.com/wolfman30/medspa-ai-platform/internal/stories"
	"github.com/wolfman30/medspa-ai-platform/internal/voice"
	"github.com/wolfman30/medspa-ai-platform/internal/webchat"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Config holds router configuration and handler dependencies.
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

	// SaaS billing (AI Wolf's own subscriptions)
	Billing        *payments.BillingHandler
	BillingWebhook *payments.BillingWebhookHandler

	// Structured knowledge handler
	StructuredKnowledgeHandler *handlers.StructuredKnowledgeHandler

	// Booking callback handler
	BookingCallbackHandler *conversation.BookingCallbackHandler

	// Short payment URL redirect handler
	PaymentRedirect *payments.RedirectHandler

	// Morning briefs handler
	AdminBriefs *handlers.AdminBriefsHandler

	// Finance dashboard (Plaid + budget)
	AdminFinance *handlers.AdminFinanceHandler

	// Research intelligence
	AdminResearch *handlers.AdminResearchHandler

	// Prospect tracker
	ProspectsHandler *prospects.Handler

	// Story board / Kanban
	StoriesHandler *stories.Handler

	// Voice AI handler (Telnyx AI Assistant webhook)
	VoiceAIHandler *handlers.VoiceAIHandler

	// GitHub Actions webhook handler
	GitHubWebhook *handlers.GitHubWebhookHandler

	// Instagram DM adapter
	InstagramAdapter *instagram.Adapter

	// Nova Sonic voice WebSocket handler
	VoiceWSHandler *voice.TelnyxWSHandler

	// Call Control handler (answers calls + starts media streaming)
	CallControlHandler *handlers.CallControlHandler

	// Web Chat handler
	WebChatHandler *webchat.Handler

	// Evidence upload S3
	EvidenceS3Client handlers.S3Uploader
	EvidenceS3Bucket string
	EvidenceS3Region string

	// Readiness check dependencies
	RedisClient    *redis.Client
	HasSMSProvider bool
}

// New creates a new Chi router with all routes configured. Route groups are
// split across separate files for readability:
//   - routes_public.go  — unauthenticated webhooks, health, OAuth
//   - routes_admin.go   — JWT-protected admin endpoints
//   - routes_portal.go  — customer portal (read-only, org-scoped)
//   - routes_tenant.go  — org-scoped API (leads, payments, conversations)
func New(cfg *Config) http.Handler {
	r := chi.NewRouter()

	// Global middleware
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

	registerPublicRoutes(r, cfg)
	registerAdminRoutes(r, cfg)
	registerPortalRoutes(r, cfg)
	registerTenantRoutes(r, cfg)

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
