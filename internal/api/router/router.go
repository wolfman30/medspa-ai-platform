package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Config holds router configuration
type Config struct {
	Logger           *logging.Logger
	LeadsHandler     *leads.Handler
	MessagingHandler *messaging.Handler
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

	// Health check
	r.Get("/health", cfg.MessagingHandler.HealthCheck)

	// Leads routes
	r.Route("/leads", func(r chi.Router) {
		r.Post("/web", cfg.LeadsHandler.CreateWebLead)
	})

	// Messaging routes
	r.Route("/messaging", func(r chi.Router) {
		r.Post("/twilio/webhook", cfg.MessagingHandler.TwilioWebhook)
	})

	return r
}
