package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/compliance"
)

// handleInbound processes an inbound SMS message: deduplication, compliance
// keyword handling (STOP/HELP/START), PCI redaction, and conversation dispatch.
func (h *TelnyxWebhookHandler) handleInbound(ctx context.Context, evt telnyxEvent) error {
	var payload telnyxMessagePayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("decode inbound payload: %w", err)
	}
	dedupeID := strings.TrimSpace(payload.MessageID)
	if dedupeID == "" {
		dedupeID = strings.TrimSpace(payload.ID)
	}
	h.logger.Info("telnyx inbound dedupe check",
		"event_id", evt.ID,
		"payload_id", payload.ID,
		"message_id", payload.MessageID,
		"dedupe_id", dedupeID,
		"direction", payload.Direction,
		"from", payload.FromNumber(),
	)
	if h.processed != nil && dedupeID != "" {
		isNew, err := h.processed.MarkProcessed(ctx, "telnyx.message_id", dedupeID)
		if err != nil {
			return fmt.Errorf("processed lookup failed: %w", err)
		}
		if !isNew {
			h.logger.Info("telnyx inbound dedupe: already processed", "dedupe_id", dedupeID)
			return nil
		}
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
	orgID := clinicID.String()
	conversationID := telnyxConversationID(orgID, from)
	seenInbound, err := h.store.HasInboundMessage(ctx, clinicID, from, to)
	if err != nil {
		return fmt.Errorf("check inbound history: %w", err)
	}
	isFirstInbound := !seenInbound
	tx, err := h.store.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	media := payload.MediaURLs

	rawBody := payload.Text
	panRedacted, sawPAN := compliance.RedactPAN(rawBody)
	storageBody, _ := conversation.RedactSensitive(panRedacted)
	msgRecord := messaging.MessageRecord{ClinicID: clinicID, From: from, To: to, Direction: "inbound", Body: storageBody, Media: media, ProviderStatus: payload.Status, ProviderMessageID: payload.ID}
	msgID, err := h.store.InsertMessage(ctx, tx, msgRecord)
	if err != nil {
		if isDuplicateProviderMessage(err) {
			h.logger.Info("telnyx inbound duplicate message ignored", "provider_message_id", payload.ID)
			return nil
		}
		return fmt.Errorf("insert inbound message: %w", err)
	}
	received := events.MessageReceivedV1{MessageID: msgID.String(), ClinicID: clinicID.String(), FromE164: from, ToE164: to, Body: storageBody, MediaURLs: media, Provider: "telnyx", ReceivedAt: evt.OccurredAt, TelnyxEventID: evt.ID}
	if _, err := events.AppendCanonicalEvent(ctx, tx, "clinic:"+clinicID.String(), evt.ID, received); err != nil {
		return fmt.Errorf("append inbound event: %w", err)
	}
	var stop, help, start bool
	if h.detector != nil {
		stop = h.detector.IsStop(payload.Text)
		help = h.detector.IsHelp(payload.Text)
		start = h.detector.IsStart(payload.Text)
	}
	if h.demoMode && strings.EqualFold(strings.TrimSpace(payload.Text), "YES") {
		start = true
	}
	unsubscribed := false
	if !stop && !start {
		unsubscribed, err = h.store.IsUnsubscribed(ctx, clinicID, from)
		if err != nil {
			return fmt.Errorf("check unsubscribe: %w", err)
		}
	}
	if stop {
		if err := h.store.InsertUnsubscribe(ctx, tx, clinicID, from, "STOP"); err != nil {
			return fmt.Errorf("record unsubscribe: %w", err)
		}
	}
	if start {
		if err := h.store.DeleteUnsubscribe(ctx, tx, clinicID, from); err != nil {
			return fmt.Errorf("record resubscribe: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit inbound tx: %w", err)
	}
	if h.metrics != nil {
		h.metrics.ObserveInbound(evt.EventType, payload.Status)
	}

	h.appendTranscript(context.Background(), conversationID, conversation.SMSTranscriptMessage{ID: msgID.String(), Role: "user", From: from, To: to, Body: storageBody, Timestamp: evt.OccurredAt, Kind: "inbound", ProviderMessageID: payload.ID})

	switch {
	case stop:
		h.appendTranscript(context.Background(), conversationID, conversation.SMSTranscriptMessage{Role: "assistant", From: to, To: from, Body: h.stopAck, Kind: "stop_ack"})
		h.sendAutoReply(context.Background(), to, from, h.stopAck)
	case help:
		h.appendTranscript(context.Background(), conversationID, conversation.SMSTranscriptMessage{Role: "assistant", From: to, To: from, Body: h.helpAck, Kind: "help_ack"})
		h.sendAutoReply(context.Background(), to, from, h.helpAck)
	case start:
		h.appendTranscript(context.Background(), conversationID, conversation.SMSTranscriptMessage{Role: "assistant", From: to, To: from, Body: h.startAck, Kind: "start_ack"})
		h.sendAutoReply(context.Background(), to, from, h.startAck)
	case unsubscribed:
		// no-op
	case sawPAN:
		h.appendTranscript(context.Background(), conversationID, conversation.SMSTranscriptMessage{Role: "assistant", From: to, To: from, Body: messaging.PCIGuardrailMessage, Kind: "pci_guardrail"})
		h.sendAutoReply(context.Background(), to, from, messaging.PCIGuardrailMessage)
	default:
		if isFirstInbound {
			ack := messaging.GetSmsAckMessage(true)
			ackKind := "ack"
			if h.demoMode && h.firstContactAck != "" {
				ack = h.firstContactAck
				ackKind = "first_contact_ack"
			}
			h.appendTranscript(context.Background(), conversationID, conversation.SMSTranscriptMessage{Role: "assistant", From: to, To: from, Body: ack, Kind: ackKind})
			h.sendAutoReply(context.Background(), to, from, ack)
		}
		h.dispatchConversation(context.Background(), evt, payload, clinicID, conversationID, panRedacted)
	}
	return nil
}

func (h *TelnyxWebhookHandler) dispatchConversation(ctx context.Context, evt telnyxEvent, payload telnyxMessagePayload, clinicID uuid.UUID, conversationID string, body string) {
	if h.conversation == nil {
		return
	}
	orgID := clinicID.String()
	from := messaging.NormalizeE164(payload.FromNumber())
	to := messaging.NormalizeE164(payload.ToNumber())
	if from == "" || to == "" || strings.TrimSpace(body) == "" {
		return
	}
	leadID := fmt.Sprintf("%s:%s", orgID, from)
	if h.leads != nil {
		lead, err := h.leads.GetOrCreateByPhone(ctx, orgID, from, "telnyx_sms", "")
		if err != nil {
			h.logger.Error("failed to persist lead for telnyx inbound", "error", err, "org_id", orgID, "from", from)
			return
		}
		if lead != nil && lead.ID != "" {
			leadID = lead.ID
		}
	}
	h.linkLead(ctx, conversationID, leadID)
	req := conversation.MessageRequest{OrgID: orgID, LeadID: leadID, ConversationID: conversationID, Message: body, ClinicID: orgID, Channel: conversation.ChannelSMS, From: from, To: to, Metadata: map[string]string{"telnyx_event_id": evt.ID, "telnyx_message_id": payload.ID, "direction": payload.Direction}}
	jobID := fmt.Sprintf("telnyx:%s", payload.ID)
	publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	opts := []conversation.PublishOption{conversation.WithoutJobTracking()}
	if h.trackJobs {
		opts = nil
	}
	if err := h.conversation.EnqueueMessage(publishCtx, jobID, req, opts...); err != nil {
		h.logger.Error("failed to enqueue telnyx conversation job", "error", err, "job_id", jobID)
	}
}
