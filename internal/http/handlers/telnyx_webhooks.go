package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wolfman30/medspa-ai-platform/internal/clinic"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	observemetrics "github.com/wolfman30/medspa-ai-platform/internal/observability/metrics"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type conversationPublisher interface {
	EnqueueStart(ctx context.Context, jobID string, req conversation.StartRequest, opts ...conversation.PublishOption) error
	EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error
}

type processedTracker interface {
	AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error)
	MarkProcessed(ctx context.Context, provider, eventID string) (bool, error)
}

type conversationStore interface {
	AppendMessage(ctx context.Context, conversationID string, msg conversation.SMSTranscriptMessage) error
	LinkLead(ctx context.Context, conversationID string, leadID uuid.UUID) error
}

type conversationStatusUpdater interface {
	UpdateMessageStatusByProviderID(ctx context.Context, providerMessageID, status, errorReason string) error
}

var errClinicNotFound = errors.New("clinic not found")

// TelnyxWebhookHandler handles inbound Telnyx webhooks for messaging and hosted orders.
type TelnyxWebhookHandler struct {
	store            messagingStore
	processed        processedTracker
	telnyx           telnyxClient
	conversation     conversationPublisher
	leads            leads.Repository
	logger           *logging.Logger
	transcript       *conversation.SMSTranscriptStore
	convStore        conversationStore
	clinicStore      *clinic.Store
	messagingProfile string
	stopAck          string
	helpAck          string
	startAck         string
	firstContactAck  string
	voiceAck         string
	demoMode         bool
	trackJobs        bool
	detector         *compliance.Detector
	metrics          *observemetrics.MessagingMetrics
}

// TelnyxWebhookConfig holds configuration for constructing a TelnyxWebhookHandler.
type TelnyxWebhookConfig struct {
	Store             messagingStore
	Processed         processedTracker
	Telnyx            telnyxClient
	Conversation      conversationPublisher
	Leads             leads.Repository
	Logger            *logging.Logger
	Transcript        *conversation.SMSTranscriptStore
	ConversationStore conversationStore
	ClinicStore       *clinic.Store
	MessagingProfile  string
	StopAck           string
	HelpAck           string
	StartAck          string
	FirstContactAck   string
	VoiceAck          string
	DemoMode          bool
	TrackJobs         bool
	Metrics           *observemetrics.MessagingMetrics
}

// NewTelnyxWebhookHandler creates a new handler with the given configuration.
func NewTelnyxWebhookHandler(cfg TelnyxWebhookConfig) *TelnyxWebhookHandler {
	if cfg.Logger == nil {
		cfg.Logger = logging.Default()
	}
	return &TelnyxWebhookHandler{
		store:            cfg.Store,
		processed:        cfg.Processed,
		telnyx:           cfg.Telnyx,
		conversation:     cfg.Conversation,
		leads:            cfg.Leads,
		logger:           cfg.Logger,
		transcript:       cfg.Transcript,
		convStore:        cfg.ConversationStore,
		clinicStore:      cfg.ClinicStore,
		messagingProfile: cfg.MessagingProfile,
		stopAck:          defaultString(cfg.StopAck, "You have been opted out. Reply HELP for info."),
		helpAck:          defaultString(cfg.HelpAck, "Reply STOP to opt out or contact support@medspa.ai."),
		startAck:         defaultString(cfg.StartAck, "You're opted back in. Reply STOP to opt out."),
		firstContactAck:  strings.TrimSpace(cfg.FirstContactAck),
		voiceAck:         defaultString(cfg.VoiceAck, messaging.InstantAckMessage),
		demoMode:         cfg.DemoMode,
		trackJobs:        cfg.TrackJobs,
		detector:         compliance.NewDetector(),
		metrics:          cfg.Metrics,
	}
}

func (h *TelnyxWebhookHandler) appendTranscript(ctx context.Context, conversationID string, msg conversation.SMSTranscriptMessage) {
	if h == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if h.transcript != nil {
		if err := h.transcript.Append(ctx, conversationID, msg); err != nil {
			h.logger.Warn("failed to append sms transcript", "error", err, "conversation_id", conversationID)
		}
	}
	if h.convStore != nil {
		if err := h.convStore.AppendMessage(ctx, conversationID, msg); err != nil {
			h.logger.Warn("failed to persist sms transcript", "error", err, "conversation_id", conversationID)
		}
	}
}

func (h *TelnyxWebhookHandler) clinicConfig(ctx context.Context, orgID string) *clinic.Config {
	if h == nil || h.clinicStore == nil {
		return nil
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := h.clinicStore.Get(ctx, orgID)
	if err != nil {
		h.logger.Warn("failed to load clinic config", "error", err, "org_id", orgID)
		return nil
	}
	return cfg
}

func (h *TelnyxWebhookHandler) clinicName(ctx context.Context, orgID string) string {
	cfg := h.clinicConfig(ctx, orgID)
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Name)
}

func (h *TelnyxWebhookHandler) voiceAckMessage(ctx context.Context, orgID string) string {
	// Check for environment-level override first
	if strings.TrimSpace(h.voiceAck) != "" && h.voiceAck != messaging.InstantAckMessage {
		return h.voiceAck
	}

	// Get clinic config to check for AIPersona greetings
	cfg := h.clinicConfig(ctx, orgID)
	if cfg != nil {
		// Use time-aware greeting: after-hours greeting when closed, custom greeting when open
		now := time.Now()
		isOpen := cfg.IsOpenAt(now)

		// If closed and has after-hours greeting, use it
		if !isOpen && strings.TrimSpace(cfg.AIPersona.AfterHoursGreeting) != "" {
			return cfg.AIPersona.AfterHoursGreeting
		}

		// If open (or no after-hours greeting) and has custom greeting, use it
		if strings.TrimSpace(cfg.AIPersona.CustomGreeting) != "" {
			return cfg.AIPersona.CustomGreeting
		}

	}

	// Fall back to standard template with clinic name (no callback option)
	name := ""
	if cfg != nil {
		name = strings.TrimSpace(cfg.Name)
	}
	return messaging.InstantAckMessageForClinic(name)
}

func (h *TelnyxWebhookHandler) linkLead(ctx context.Context, conversationID, leadID string) {
	if h == nil || h.convStore == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	leadUUID, err := uuid.Parse(strings.TrimSpace(leadID))
	if err != nil {
		return
	}
	if err := h.convStore.LinkLead(ctx, conversationID, leadUUID); err != nil {
		h.logger.Warn("failed to link lead to conversation", "error", err, "conversation_id", conversationID, "lead_id", leadID)
	}
}

// HandleMessages processes Telnyx message webhooks (inbound messages + delivery receipts).
func (h *TelnyxWebhookHandler) HandleMessages(w http.ResponseWriter, r *http.Request) {
	if h.telnyx == nil {
		http.Error(w, "telnyx client not configured", http.StatusServiceUnavailable)
		return
	}
	start := time.Now()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := h.telnyx.VerifyWebhookSignature(r.Header.Get("Telnyx-Timestamp"), r.Header.Get("Telnyx-Signature"), body); err != nil {
		h.logger.Warn("invalid telnyx webhook signature", "error", err)
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	evt, err := parseTelnyxEvent(body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if processed, err := h.processed.AlreadyProcessed(r.Context(), "telnyx", evt.ID); err != nil {
		h.logger.Error("processed lookup failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	} else if processed {
		w.WriteHeader(http.StatusOK)
		return
	}
	var handlerErr error
	switch evt.EventType {
	case "message.received":
		handlerErr = h.handleInbound(r.Context(), evt)
	case "message.delivery_status":
		handlerErr = h.handleDeliveryStatus(r.Context(), evt)
	default:
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if handlerErr != nil {
		if errors.Is(handlerErr, errClinicNotFound) {
			http.Error(w, handlerErr.Error(), http.StatusNotFound)
			return
		}
		h.logger.Error("telnyx webhook handling failed", "error", handlerErr, "event_type", evt.EventType)
		http.Error(w, "processing error", http.StatusInternalServerError)
		return
	}
	if h.metrics != nil {
		h.metrics.ObserveWebhookLatency(evt.EventType, time.Since(start).Seconds())
	}
	if _, err := h.processed.MarkProcessed(r.Context(), "telnyx", evt.ID); err != nil {
		h.logger.Error("failed to mark telnyx event processed", "error", err, "event_id", evt.ID)
	}
	w.WriteHeader(http.StatusOK)
}

// HandleHosted processes hosted messaging order lifecycle events.
func (h *TelnyxWebhookHandler) HandleHosted(w http.ResponseWriter, r *http.Request) {
	if h.telnyx == nil {
		http.Error(w, "telnyx client not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := h.telnyx.VerifyWebhookSignature(r.Header.Get("Telnyx-Timestamp"), r.Header.Get("Telnyx-Signature"), body); err != nil {
		h.logger.Warn("invalid telnyx hosted signature", "error", err)
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	evt, err := parseTelnyxEvent(body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if processed, err := h.processed.AlreadyProcessed(r.Context(), "telnyx", evt.ID); err != nil {
		h.logger.Error("processed lookup failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	} else if processed {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := h.handleHostedOrder(r.Context(), evt); err != nil {
		h.logger.Error("hosted order event failed", "error", err)
		http.Error(w, "processing error", http.StatusInternalServerError)
		return
	}
	if _, err := h.processed.MarkProcessed(r.Context(), "telnyx", evt.ID); err != nil {
		h.logger.Error("failed to mark telnyx event processed", "error", err, "event_id", evt.ID)
	}
	w.WriteHeader(http.StatusOK)
}

// HandleVoice processes Telnyx hosted voice webhooks for missed-call triggers.
func (h *TelnyxWebhookHandler) HandleVoice(w http.ResponseWriter, r *http.Request) {
	if h.telnyx == nil {
		http.Error(w, "telnyx client not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := h.telnyx.VerifyWebhookSignature(r.Header.Get("Telnyx-Timestamp"), r.Header.Get("Telnyx-Signature"), body); err != nil {
		h.logger.Warn("invalid telnyx voice signature", "error", err)
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	evt, err := parseTelnyxEvent(body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if processed, err := h.processed.AlreadyProcessed(r.Context(), "telnyx", evt.ID); err != nil {
		h.logger.Error("processed lookup failed", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	} else if processed {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := h.handleVoice(r.Context(), evt); err != nil {
		if errors.Is(err, errClinicNotFound) {
			h.logger.Warn("telnyx voice: clinic not found", "error", err, "event_type", evt.EventType)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		h.logger.Error("telnyx voice handling failed", "error", err, "event_type", evt.EventType)
		http.Error(w, "processing error", http.StatusInternalServerError)
		return
	}
	if _, err := h.processed.MarkProcessed(r.Context(), "telnyx", evt.ID); err != nil {
		h.logger.Error("failed to mark telnyx voice processed", "error", err, "event_id", evt.ID)
	}
	w.WriteHeader(http.StatusOK)
}

func (h *TelnyxWebhookHandler) sendAutoReply(ctx context.Context, from, to, body string) {
	if body == "" || h.messagingProfile == "" || h.telnyx == nil {
		return
	}
	payload := telnyxclient.SendMessageRequest{
		From:               from,
		To:                 to,
		Body:               body,
		MessagingProfileID: h.messagingProfile,
	}
	if _, err := h.telnyx.SendMessage(ctx, payload); err != nil {
		h.logger.Warn("failed to send auto-reply", "error", err)
	}
}
