package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/leads"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/compliance"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	observemetrics "github.com/wolfman30/medspa-ai-platform/internal/observability/metrics"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type conversationPublisher interface {
	EnqueueMessage(ctx context.Context, jobID string, req conversation.MessageRequest, opts ...conversation.PublishOption) error
}

type processedTracker interface {
	AlreadyProcessed(ctx context.Context, provider, eventID string) (bool, error)
	MarkProcessed(ctx context.Context, provider, eventID string) (bool, error)
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
	messagingProfile string
	stopAck          string
	helpAck          string
	detector         *compliance.Detector
	metrics          *observemetrics.MessagingMetrics
}

type TelnyxWebhookConfig struct {
	Store            messagingStore
	Processed        processedTracker
	Telnyx           telnyxClient
	Conversation     conversationPublisher
	Leads            leads.Repository
	Logger           *logging.Logger
	MessagingProfile string
	StopAck          string
	HelpAck          string
	Metrics          *observemetrics.MessagingMetrics
}

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
		messagingProfile: cfg.MessagingProfile,
		stopAck:          defaultString(cfg.StopAck, "You have been opted out. Reply HELP for info."),
		helpAck:          defaultString(cfg.HelpAck, "Reply STOP to opt out or contact support@medspa.ai."),
		detector:         compliance.NewDetector(),
		metrics:          cfg.Metrics,
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
		http.Error(w, "invalid signature", http.StatusUnauthorized)
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
		http.Error(w, "invalid signature", http.StatusUnauthorized)
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

func (h *TelnyxWebhookHandler) handleInbound(ctx context.Context, evt telnyxEvent) error {
	var payload telnyxMessagePayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("decode inbound payload: %w", err)
	}
	from := messaging.NormalizeE164(payload.FromNumber())
	to := messaging.NormalizeE164(payload.ToNumber())
	if from == "" || to == "" {
		return errors.New("missing phone numbers in payload")
	}
	clinicID, err := h.store.LookupClinicByNumber(ctx, to)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", errClinicNotFound, to)
		}
		return fmt.Errorf("lookup clinic for %s: %w", to, err)
	}
	tx, err := h.store.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	media := payload.MediaURLs
	msgRecord := messaging.MessageRecord{
		ClinicID:          clinicID,
		From:              from,
		To:                to,
		Direction:         "inbound",
		Body:              payload.Text,
		Media:             media,
		ProviderStatus:    payload.Status,
		ProviderMessageID: payload.ID,
	}
	msgID, err := h.store.InsertMessage(ctx, tx, msgRecord)
	if err != nil {
		return fmt.Errorf("insert inbound message: %w", err)
	}
	received := events.MessageReceivedV1{
		MessageID:     msgID.String(),
		ClinicID:      clinicID.String(),
		FromE164:      from,
		ToE164:        to,
		Body:          payload.Text,
		MediaURLs:     media,
		Provider:      "telnyx",
		ReceivedAt:    evt.OccurredAt,
		TelnyxEventID: evt.ID,
	}
	if _, err := events.AppendCanonicalEvent(ctx, tx, "clinic:"+clinicID.String(), evt.ID, received); err != nil {
		return fmt.Errorf("append inbound event: %w", err)
	}
	var stop bool
	var help bool
	if h.detector != nil {
		stop = h.detector.IsStop(payload.Text)
		help = h.detector.IsHelp(payload.Text)
	}
	if stop {
		if err := h.store.InsertUnsubscribe(ctx, tx, clinicID, from, "STOP"); err != nil {
			return fmt.Errorf("record unsubscribe: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit inbound tx: %w", err)
	}
	if h.metrics != nil {
		h.metrics.ObserveInbound(evt.EventType, payload.Status)
	}
	if stop {
		h.sendAutoReply(context.Background(), to, from, h.stopAck)
	} else if help {
		h.sendAutoReply(context.Background(), to, from, h.helpAck)
	} else {
		h.sendAutoReply(context.Background(), to, from, messaging.InstantAckMessage)
		h.dispatchConversation(context.Background(), evt, payload, clinicID)
	}
	return nil
}

func (h *TelnyxWebhookHandler) handleDeliveryStatus(ctx context.Context, evt telnyxEvent) error {
	var payload telnyxDeliveryPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("decode delivery payload: %w", err)
	}
	var deliveredAt, failedAt *time.Time
	switch strings.ToLower(payload.Status) {
	case "delivered":
		deliveredAt = &evt.OccurredAt
	case "undelivered", "failed":
		failedAt = &evt.OccurredAt
	}
	if err := h.store.UpdateMessageStatus(ctx, payload.MessageID, payload.Status, deliveredAt, failedAt); err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	if h.metrics != nil {
		h.metrics.ObserveInbound(evt.EventType, payload.Status)
	}
	return nil
}

func (h *TelnyxWebhookHandler) handleHostedOrder(ctx context.Context, evt telnyxEvent) error {
	var payload telnyxHostedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("decode hosted payload: %w", err)
	}
	clinicID, err := uuid.Parse(payload.ClinicID)
	if err != nil {
		return fmt.Errorf("clinic id parse: %w", err)
	}
	tx, err := h.store.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin hosted tx: %w", err)
	}
	defer tx.Rollback(ctx)
	record := messaging.HostedOrderRecord{
		ClinicID:        clinicID,
		E164Number:      payload.PhoneNumber,
		Status:          payload.Status,
		LastError:       payload.LastError,
		ProviderOrderID: payload.ID,
	}
	if err := h.store.UpsertHostedOrder(ctx, tx, record); err != nil {
		return fmt.Errorf("persist hosted order: %w", err)
	}
	if strings.EqualFold(payload.Status, "activated") {
		activated := events.HostedOrderActivatedV1{
			OrderID:     payload.ID,
			ClinicID:    clinicID.String(),
			E164Number:  payload.PhoneNumber,
			ActivatedAt: evt.OccurredAt,
		}
		if _, err := events.AppendCanonicalEvent(ctx, tx, "clinic:"+clinicID.String(), evt.ID, activated); err != nil {
			return fmt.Errorf("append activation event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit hosted tx: %w", err)
	}
	if h.metrics != nil {
		h.metrics.ObserveInbound(evt.EventType, payload.Status)
	}
	return nil
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

type telnyxEvent struct {
	ID         string          `json:"id"`
	EventType  string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

func parseTelnyxEvent(body []byte) (telnyxEvent, error) {
	// Try event-driven format first (with data wrapper)
	var wrapper struct {
		Data struct {
			ID         string          `json:"id"`
			EventType  string          `json:"event_type"`
			OccurredAt time.Time       `json:"occurred_at"`
			Payload    json.RawMessage `json:"payload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Data.ID != "" {
		return telnyxEvent{
			ID:         wrapper.Data.ID,
			EventType:  wrapper.Data.EventType,
			OccurredAt: wrapper.Data.OccurredAt,
			Payload:    wrapper.Data.Payload,
		}, nil
	}

	// Try message record format (no wrapper)
	var record struct {
		ID         string    `json:"id"`
		RecordType string    `json:"record_type"`
		ReceivedAt time.Time `json:"received_at"`
		Direction  string    `json:"direction"`
	}
	if err := json.Unmarshal(body, &record); err != nil {
		return telnyxEvent{}, err
	}

	// Convert to event format
	eventType := ""
	if record.RecordType == "message" && record.Direction == "inbound" {
		eventType = "message.received"
	} else if record.RecordType == "message" && record.Direction == "outbound" {
		eventType = "message.delivery_status"
	}

	return telnyxEvent{
		ID:         record.ID,
		EventType:  eventType,
		OccurredAt: record.ReceivedAt,
		Payload:    body, // Use the whole body as payload
	}, nil
}

type telnyxMessagePayload struct {
	ID        string   `json:"id"`
	Direction string   `json:"direction"`
	Text      string   `json:"text"`
	MediaURLs []string `json:"media_urls"`
	Status    string   `json:"status"`
	From      struct {
		PhoneNumber string `json:"phone_number"`
	} `json:"from"`
	To []struct {
		PhoneNumber string `json:"phone_number"`
	} `json:"to"`
	FromNumberRaw string `json:"from_number"`
	ToNumberRaw   string `json:"to_number"`
	MessageID     string `json:"message_id"`
}

func (p telnyxMessagePayload) FromNumber() string {
	if v := strings.TrimSpace(p.From.PhoneNumber); v != "" {
		return v
	}
	return strings.TrimSpace(p.FromNumberRaw)
}

func (p telnyxMessagePayload) ToNumber() string {
	if len(p.To) > 0 {
		if v := strings.TrimSpace(p.To[0].PhoneNumber); v != "" {
			return v
		}
	}
	return strings.TrimSpace(p.ToNumberRaw)
}

type telnyxDeliveryPayload struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type telnyxHostedPayload struct {
	ID          string `json:"id"`
	ClinicID    string `json:"clinic_id"`
	PhoneNumber string `json:"phone_number"`
	Status      string `json:"status"`
	LastError   string `json:"last_error"`
}

func (h *TelnyxWebhookHandler) dispatchConversation(ctx context.Context, evt telnyxEvent, payload telnyxMessagePayload, clinicID uuid.UUID) {
	if h.conversation == nil {
		return
	}
	orgID := clinicID.String()
	from := messaging.NormalizeE164(payload.FromNumber())
	to := messaging.NormalizeE164(payload.ToNumber())
	if from == "" || to == "" || strings.TrimSpace(payload.Text) == "" {
		return
	}
	leadID := fmt.Sprintf("%s:%s", orgID, from)
	if h.leads != nil {
		lead, err := h.leads.GetOrCreateByPhone(ctx, orgID, from, "telnyx_sms", from)
		if err != nil {
			h.logger.Error("failed to persist lead for telnyx inbound", "error", err, "org_id", orgID, "from", from)
			return
		}
		if lead != nil && lead.ID != "" {
			leadID = lead.ID
		}
	}
	req := conversation.MessageRequest{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: fmt.Sprintf("sms:%s:%s", orgID, from),
		Message:        payload.Text,
		Channel:        conversation.ChannelSMS,
		From:           from,
		To:             to,
		Metadata: map[string]string{
			"telnyx_event_id":   evt.ID,
			"telnyx_message_id": payload.ID,
			"direction":         payload.Direction,
		},
	}
	jobID := fmt.Sprintf("telnyx:%s", payload.ID)
	publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := h.conversation.EnqueueMessage(publishCtx, jobID, req, conversation.WithoutJobTracking()); err != nil {
		h.logger.Error("failed to enqueue telnyx conversation job", "error", err, "job_id", jobID)
	}
}
