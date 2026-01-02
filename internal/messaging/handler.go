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

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

var twilioTracer = otel.Tracer("medspa.internal.messaging.twilio")

type conversationPublisher interface {
	EnqueueStart(ctx context.Context, jobID string, req conversation.StartRequest, opts ...conversation.PublishOption) error
	EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error
}

type conversationStore interface {
	AppendMessage(ctx context.Context, conversationID string, msg conversation.SMSTranscriptMessage) error
	LinkLead(ctx context.Context, conversationID string, leadID uuid.UUID) error
}

// Handler handles messaging webhook requests.
type Handler struct {
	webhookSecret string
	publisher     conversationPublisher
	orgResolver   OrgResolver
	messenger     conversation.ReplyMessenger
	leads         leads.Repository
	convStore     conversationStore
	clinicStore   *clinic.Store
	skipSignature bool
	publicBaseURL string
	logger        *logging.Logger
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
	}
}

// SetConversationStore attaches a persistent conversation store for inbound SMS history.
func (h *Handler) SetConversationStore(store conversationStore) {
	if h == nil {
		return
	}
	h.convStore = store
}

// SetClinicStore attaches a clinic config store for personalized messaging.
func (h *Handler) SetClinicStore(store *clinic.Store) {
	if h == nil {
		return
	}
	h.clinicStore = store
}

// SetPublicBaseURL configures the externally-visible base URL for webhook signature validation.
func (h *Handler) SetPublicBaseURL(baseURL string) {
	if h == nil {
		return
	}
	h.publicBaseURL = strings.TrimSpace(baseURL)
}

// SetSkipSignature disables Twilio signature validation (dev/testing only).
func (h *Handler) SetSkipSignature(skip bool) {
	if h == nil {
		return
	}
	h.skipSignature = skip
	if skip && h.logger != nil {
		h.logger.Warn("twilio signature validation disabled")
	}
}

// TwilioWebhook handles POST /messaging/twilio/webhook requests.
func (h *Handler) TwilioWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, span := twilioTracer.Start(r.Context(), "messaging.twilio.webhook")
	defer span.End()

	webhookURL := buildAbsoluteURL(r, h.publicBaseURL)
	if h.webhookSecret != "" && !h.skipSignature {
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
	leadID, isNewLead, err := h.ensureLead(r.Context(), orgID, from, "twilio_sms")
	if err != nil {
		h.logger.Error("failed to persist lead", "error", err, "org_id", orgID, "from", from)
		http.Error(w, "Failed to persist lead", http.StatusInternalServerError)
		span.RecordError(err)
		return
	}
	conversationID := deterministicConversationID(orgID, from)
	redactedBody, _ := conversation.RedactSensitive(webhook.Body)

	h.appendConversationMessage(ctx, conversationID, conversation.SMSTranscriptMessage{
		ID:   twilioMessageUUID(webhook.MessageSid),
		Role: "user",
		From: from,
		To:   to,
		Body: redactedBody,
		Kind: "inbound",
	})
	h.linkLead(ctx, conversationID, leadID)
	h.sendSMSAck(from, to, orgID, leadID, conversationID, webhook.MessageSid, isNewLead)

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
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response></Response>`))
}

func (h *Handler) sendSMSAck(to, from, orgID, leadID, conversationID, messageSid string, isNewLead bool) {
	if h.messenger == nil {
		return
	}
	if strings.TrimSpace(to) == "" || strings.TrimSpace(from) == "" {
		return
	}

	ackMsg := GetSmsAckMessage(isNewLead)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reply := conversation.OutboundReply{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		To:             to,
		From:           from,
		Body:           ackMsg,
		Metadata: map[string]string{
			"twilio_message_sid": messageSid,
			"kind":               "sms_ack",
		},
	}
	if err := h.messenger.SendReply(ctx, reply); err != nil {
		h.logger.Warn("failed to send sms ack", "error", err, "org_id", orgID)
	}
	h.appendConversationMessage(context.Background(), conversationID, conversation.SMSTranscriptMessage{
		Role: "assistant",
		From: from,
		To:   to,
		Body: ackMsg,
		Kind: "sms_ack",
	})
}

// TwilioVoiceWebhook handles POST /webhooks/twilio/voice for missed-call detection.
func (h *Handler) TwilioVoiceWebhook(w http.ResponseWriter, r *http.Request) {
	ctx, span := twilioTracer.Start(r.Context(), "messaging.twilio.voice")
	defer span.End()

	webhookURL := buildAbsoluteURL(r, h.publicBaseURL)
	if h.webhookSecret != "" && !h.skipSignature {
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
		// Return TwiML that rejects the call - this will trigger a status callback
		// with "no-answer" status, which will then trigger the missed call SMS flow
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response><Reject reason="busy"/></Response>`))
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

	leadID, _, err := h.ensureLead(r.Context(), orgID, from, "twilio_voice")
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
		Silent:         true,
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

	ackMsg := InstantAckMessageForClinic(h.clinicName(ctx, orgID))
	reply := conversation.OutboundReply{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		To:             to,
		From:           from,
		Body:           ackMsg,
		Metadata: map[string]string{
			"twilio_call_sid": callSid,
			"kind":            "missed_call_ack",
		},
	}
	if err := h.messenger.SendReply(ctx, reply); err != nil {
		h.logger.Warn("failed to send missed-call ack sms", "error", err, "org_id", orgID, "call_sid", callSid)
	}
}

func (h *Handler) clinicName(ctx context.Context, orgID string) string {
	if h == nil || h.clinicStore == nil {
		return ""
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := h.clinicStore.Get(ctx, orgID)
	if err != nil {
		h.logger.Warn("failed to load clinic config", "error", err, "org_id", orgID)
		return ""
	}
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Name)
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

func (h *Handler) appendConversationMessage(ctx context.Context, conversationID string, msg conversation.SMSTranscriptMessage) {
	if h == nil || h.convStore == nil {
		return
	}
	if strings.TrimSpace(conversationID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := h.convStore.AppendMessage(ctx, conversationID, msg); err != nil {
		h.logger.Warn("failed to persist sms transcript", "error", err, "conversation_id", conversationID)
	}
}

func (h *Handler) linkLead(ctx context.Context, conversationID, leadID string) {
	if h == nil || h.convStore == nil {
		return
	}
	leadUUID, err := uuid.Parse(strings.TrimSpace(leadID))
	if err != nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := h.convStore.LinkLead(ctx, conversationID, leadUUID); err != nil {
		h.logger.Warn("failed to link lead to conversation", "error", err, "conversation_id", conversationID, "lead_id", leadID)
	}
}

func twilioMessageUUID(messageSid string) string {
	messageSid = strings.TrimSpace(messageSid)
	if messageSid == "" {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("twilio:"+messageSid)).String()
}

// ensureLead returns (leadID, isNewLead, error)
func (h *Handler) ensureLead(ctx context.Context, orgID, phone, source string) (string, bool, error) {
	normalized := NormalizeE164(phone)
	if normalized == "" {
		normalized = phone
	}
	if h.leads == nil {
		return deterministicLeadID(orgID, normalized), true, nil
	}
	lead, err := h.leads.GetOrCreateByPhone(ctx, orgID, normalized, source, normalized)
	if err != nil {
		return "", false, err
	}
	// Check if lead was just created (created_at within last few seconds indicates new)
	isNew := !lead.CreatedAt.IsZero() && time.Since(lead.CreatedAt) < 5*time.Second
	return lead.ID, isNew, nil
}

func buildAbsoluteURL(r *http.Request, publicBaseURL string) string {
	if r.URL == nil {
		return ""
	}
	if base := strings.TrimSpace(publicBaseURL); base != "" {
		return strings.TrimRight(base, "/") + r.URL.RequestURI()
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
