package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var twilioTracer = otel.Tracer("medspa.internal.messaging.twilio")

type conversationPublisher interface {
	EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error
}

// Handler handles messaging webhook requests.
type Handler struct {
	webhookSecret string
	publisher     conversationPublisher
	orgResolver   OrgResolver
	logger        *logging.Logger

	twimlAck string
}

// NewHandler creates a new messaging handler.
func NewHandler(webhookSecret string, publisher conversationPublisher, resolver OrgResolver, logger *logging.Logger) *Handler {
	if logger == nil {
		logger = logging.Default()
	}
	if publisher == nil {
		panic("messaging: publisher cannot be nil")
	}
	if resolver == nil {
		panic("messaging: org resolver cannot be nil")
	}
	return &Handler{
		webhookSecret: webhookSecret,
		publisher:     publisher,
		orgResolver:   resolver,
		logger:        logger,
		twimlAck:      `<?xml version="1.0" encoding="UTF-8"?><Response><Message>Thanks! Our MedSpa concierge is on it.</Message></Response>`,
	}
}

// TwilioWebhook handles POST /messaging/twilio/webhook requests.
func (h *Handler) TwilioWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, span := twilioTracer.Start(r.Context(), "messaging.twilio.webhook")
	defer span.End()

	webhookURL := buildAbsoluteURL(r)
	if h.webhookSecret != "" {
		if !ValidateTwilioSignature(r, h.webhookSecret, webhookURL) {
			h.logger.Warn("invalid twilio signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			span.RecordError(errors.New("invalid twilio signature"))
			return
		}
	}

	webhook, err := ParseTwilioWebhook(r)
	if err != nil {
		h.logger.Error("failed to parse twilio webhook", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		span.RecordError(err)
		return
	}
	span.SetAttributes(
		attribute.String("medspa.twilio.message_sid", webhook.MessageSid),
		attribute.String("medspa.twilio.from", webhook.From),
		attribute.String("medspa.twilio.to", webhook.To),
	)

	if webhook.MessageSid == "" || webhook.From == "" || webhook.Body == "" {
		err := errors.New("missing required twilio fields")
		h.logger.Error("invalid twilio payload", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		span.RecordError(err)
		return
	}

	orgID, err := h.orgResolver.ResolveOrgID(ctx, webhook.To)
	if err != nil {
		h.logger.Error("failed to resolve org for twilio number", "error", err, "to", webhook.To)
		http.Error(w, "Unknown destination number", http.StatusBadRequest)
		span.RecordError(err)
		return
	}
	span.SetAttributes(attribute.String("medspa.org_id", orgID))

	jobID := webhook.MessageSid
	leadID := deterministicLeadID(orgID, webhook.From)
	conversationID := deterministicConversationID(orgID, webhook.From)

	msgReq := conversation.MessageRequest{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		Message:        webhook.Body,
		Channel:        conversation.ChannelSMS,
		From:           webhook.From,
		To:             webhook.To,
		Metadata: map[string]string{
			"twilio_message_sid": webhook.MessageSid,
			"twilio_account_sid": webhook.AccountSid,
		},
	}

	publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := h.publisher.EnqueueMessage(publishCtx, jobID, msgReq, conversation.WithoutJobTracking()); err != nil {
		h.logger.Error("failed to enqueue conversation job", "error", err, "org_id", orgID, "message_sid", webhook.MessageSid)
		http.Error(w, "Failed to schedule reply", http.StatusInternalServerError)
		span.RecordError(err)
		return
	}

	h.logger.Info("twilio webhook accepted", "org_id", orgID, "lead_id", leadID, "conversation_id", conversationID)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.twimlAck))
}

// HealthCheck returns a simple health check response.
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "ok",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func deterministicLeadID(orgID, from string) string {
	return fmt.Sprintf("%s:%s", orgID, sanitizePhone(from))
}

func deterministicConversationID(orgID, from string) string {
	return fmt.Sprintf("sms:%s:%s", orgID, sanitizePhone(from))
}

func buildAbsoluteURL(r *http.Request) string {
	if r.URL == nil {
		return ""
	}
	if r.URL.Scheme != "" {
		return r.URL.String()
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.RequestURI())
}
