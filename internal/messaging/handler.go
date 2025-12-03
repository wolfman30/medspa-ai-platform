package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var twilioTracer = otel.Tracer("medspa.internal.messaging.twilio")

type conversationPublisher interface {
	EnqueueStart(ctx context.Context, jobID string, req conversation.StartRequest, opts ...conversation.PublishOption) error
	EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error
}

// Handler handles messaging webhook requests.
type Handler struct {
	webhookSecret string
	publisher     conversationPublisher
	orgResolver   OrgResolver
	messenger     conversation.ReplyMessenger
	leads         leads.Repository
	logger        *logging.Logger

	twimlAck string
}

// NewHandler creates a new messaging handler.
func NewHandler(webhookSecret string, publisher conversationPublisher, resolver OrgResolver, messenger conversation.ReplyMessenger, leadsRepo leads.Repository, logger *logging.Logger) *Handler {
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
		messenger:     messenger,
		leads:         leadsRepo,
		logger:        logger,
		twimlAck:      `<?xml version="1.0" encoding="UTF-8"?><Response><Message>` + SmsAckMessage + `</Message></Response>`,
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
	from := NormalizeE164(webhook.From)
	to := NormalizeE164(webhook.To)
	span.SetAttributes(
		attribute.String("medspa.twilio.message_sid", webhook.MessageSid),
		attribute.String("medspa.twilio.from", from),
		attribute.String("medspa.twilio.to", to),
	)

	if webhook.MessageSid == "" || from == "" || webhook.Body == "" {
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
	leadID, err := h.ensureLead(r.Context(), orgID, from, "twilio_sms")
	if err != nil {
		h.logger.Error("failed to persist lead", "error", err, "org_id", orgID, "from", from)
		http.Error(w, "Failed to persist lead", http.StatusInternalServerError)
		span.RecordError(err)
		return
	}
	conversationID := deterministicConversationID(orgID, from)

	msgReq := conversation.MessageRequest{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		Message:        webhook.Body,
		Channel:        conversation.ChannelSMS,
		From:           from,
		To:             to,
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

// TwilioVoiceWebhook handles POST /webhooks/twilio/voice for missed-call detection.
func (h *Handler) TwilioVoiceWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, span := twilioTracer.Start(r.Context(), "messaging.twilio.voice")
	defer span.End()

	webhookURL := buildAbsoluteURL(r)
	if h.webhookSecret != "" {
		if !ValidateTwilioSignature(r, h.webhookSecret, webhookURL) {
			h.logger.Warn("invalid twilio voice signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			span.RecordError(errors.New("invalid twilio voice signature"))
			return
		}
	}
	if err := r.ParseForm(); err != nil {
		h.logger.Error("failed to parse twilio voice form", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		span.RecordError(err)
		return
	}

	callSid := strings.TrimSpace(r.FormValue("CallSid"))
	callStatus := strings.ToLower(strings.TrimSpace(r.FormValue("CallStatus")))
	from := NormalizeE164(r.FormValue("From"))
	to := NormalizeE164(r.FormValue("To"))
	if callSid == "" || from == "" || to == "" {
		err := errors.New("missing required twilio voice fields")
		h.logger.Error("invalid twilio voice payload", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		span.RecordError(err)
		return
	}
	if !isMissedCallStatus(callStatus) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	orgID, err := h.orgResolver.ResolveOrgID(ctx, to)
	if err != nil {
		h.logger.Error("failed to resolve org for twilio voice number", "error", err, "to", to)
		http.Error(w, "Unknown destination number", http.StatusBadRequest)
		span.RecordError(err)
		return
	}
	span.SetAttributes(
		attribute.String("medspa.org_id", orgID),
		attribute.String("medspa.twilio.call_sid", callSid),
		attribute.String("medspa.twilio.from", from),
		attribute.String("medspa.twilio.to", to),
		attribute.String("medspa.twilio.call_status", callStatus),
	)

	leadID, err := h.ensureLead(r.Context(), orgID, from, "twilio_voice")
	if err != nil {
		h.logger.Error("failed to persist lead", "error", err, "org_id", orgID, "from", from)
		http.Error(w, "Failed to persist lead", http.StatusInternalServerError)
		span.RecordError(err)
		return
	}
	conversationID := deterministicConversationID(orgID, from)

	startReq := conversation.StartRequest{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		Intro:          "We just missed your call. I can help you book an appointment or answer questions right here.",
		Source:         "twilio_voice",
		Channel:        conversation.ChannelSMS,
		From:           from,
		To:             to,
		Metadata: map[string]string{
			"twilio_call_sid":    callSid,
			"twilio_call_status": callStatus,
		},
	}

	publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := h.publisher.EnqueueStart(publishCtx, callSid, startReq, conversation.WithoutJobTracking()); err != nil {
		h.logger.Error("failed to enqueue missed-call conversation start", "error", err, "org_id", orgID, "call_sid", callSid)
		http.Error(w, "Failed to schedule reply", http.StatusInternalServerError)
		span.RecordError(err)
		return
	}

	h.sendImmediateAck(from, to, orgID, leadID, conversationID, callSid)

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response></Response>`))
}

func (h *Handler) sendImmediateAck(to, from, orgID, leadID, conversationID, callSid string) {
	if h.messenger == nil {
		return
	}
	if strings.TrimSpace(to) == "" || strings.TrimSpace(from) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reply := conversation.OutboundReply{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		To:             to,
		From:           from,
		Body:           InstantAckMessage,
		Metadata: map[string]string{
			"twilio_call_sid": callSid,
			"kind":            "missed_call_ack",
		},
	}
	if err := h.messenger.SendReply(ctx, reply); err != nil {
		h.logger.Warn("failed to send missed-call ack sms", "error", err, "org_id", orgID, "call_sid", callSid)
	}
}

func isMissedCallStatus(status string) bool {
	switch status {
	case "no-answer", "busy", "failed", "canceled":
		return true
	default:
		return false
	}
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

func (h *Handler) ensureLead(ctx context.Context, orgID, phone, source string) (string, error) {
	normalized := NormalizeE164(phone)
	if normalized == "" {
		normalized = phone
	}
	if h.leads == nil {
		return deterministicLeadID(orgID, normalized), nil
	}
	lead, err := h.leads.GetOrCreateByPhone(ctx, orgID, normalized, source, normalized)
	if err != nil {
		return "", err
	}
	return lead.ID, nil
}

func buildAbsoluteURL(r *http.Request) string {
	if r.URL == nil {
		return ""
	}
	if r.URL.Scheme != "" {
		return r.URL.String()
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "https"
		if r.TLS == nil {
			scheme = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return fmt.Sprintf("%s://%s%s", scheme, host, r.URL.RequestURI())
}
