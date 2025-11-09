package messaging

import (
	"encoding/json"
	"net/http"

	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

// Handler handles messaging webhook requests
type Handler struct {
	webhookSecret string
	logger        *logging.Logger
}

// NewHandler creates a new messaging handler
func NewHandler(webhookSecret string, logger *logging.Logger) *Handler {
	return &Handler{
		webhookSecret: webhookSecret,
		logger:        logger,
	}
}

// TwilioWebhook handles POST /messaging/twilio/webhook requests
func (h *Handler) TwilioWebhook(w http.ResponseWriter, r *http.Request) {
	// Validate signature if secret is configured
	if h.webhookSecret != "" {
		webhookURL := r.URL.String()
		if r.URL.Scheme == "" {
			// Construct full URL for signature validation
			scheme := "https"
			if r.TLS == nil {
				scheme = "http"
			}
			webhookURL = scheme + "://" + r.Host + r.URL.Path
		}

		if !ValidateTwilioSignature(r, h.webhookSecret, webhookURL) {
			h.logger.Warn("invalid twilio signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Parse webhook
	webhook, err := ParseTwilioWebhook(r)
	if err != nil {
		h.logger.Error("failed to parse twilio webhook", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	h.logger.Info("received twilio webhook",
		"message_sid", webhook.MessageSid,
		"from", webhook.From,
		"to", webhook.To,
		"body", webhook.Body,
	)

	// TODO: Process the message (store, trigger AI response, etc.)

	// Return TwiML response
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response></Response>`))
}

// HealthCheck returns a simple health check response
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "ok",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
