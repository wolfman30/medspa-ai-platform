package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/conversation"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
)

// handleVoice processes a voice webhook event (call.hangup, etc.) and triggers
// the missed-call text-back flow when appropriate.
func (h *TelnyxWebhookHandler) handleVoice(ctx context.Context, evt telnyxEvent) error {
	var payload telnyxCallPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("decode voice payload: %w", err)
	}
	from := messaging.NormalizeE164(payload.FromNumber())
	to := messaging.NormalizeE164(payload.ToNumber())
	h.logger.Info("voice webhook received",
		"event_type", evt.EventType,
		"from_raw", payload.FromNumber(),
		"to_raw", payload.ToNumber(),
		"from_e164", from,
		"to_e164", to,
		"status", payload.Status,
		"hangup_cause", payload.HangupCause,
	)
	if from == "" || to == "" {
		return errors.New("missing phone numbers in payload")
	}
	if !isTelnyxMissedCall(evt.EventType, payload.Status, payload.HangupCause) {
		h.logger.Info("voice webhook: not a missed call, skipping", "event_type", evt.EventType, "status", payload.Status)
		return nil
	}
	h.logger.Info("voice webhook: missed call detected, looking up clinic", "to", to)
	clinicID, err := h.store.LookupClinicByNumber(ctx, to)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.logger.Warn("voice webhook: clinic not found for number", "to", to)
			return fmt.Errorf("%w: %s", errClinicNotFound, to)
		}
		return fmt.Errorf("lookup clinic for %s: %w", to, err)
	}
	orgID := clinicID.String()
	leadID := fmt.Sprintf("%s:%s", orgID, from)
	if h.leads != nil {
		// Pass empty defaultName - name will be extracted from conversation later
		lead, err := h.leads.GetOrCreateByPhone(ctx, orgID, from, "telnyx_voice", "")
		if err != nil {
			return fmt.Errorf("persist lead: %w", err)
		}
		if lead != nil && lead.ID != "" {
			leadID = lead.ID
		}
	}
	conversationID := telnyxConversationID(orgID, from)
	// Get ack message first so we can include it in the StartRequest for history
	ack := h.voiceAckMessage(ctx, orgID)
	startReq := conversation.StartRequest{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		Intro:          "We just missed your call. I can help you book an appointment or answer quick questions by text.",
		Source:         "telnyx_voice",
		ClinicID:       orgID,
		Channel:        conversation.ChannelSMS,
		From:           from,
		To:             to,
		Silent:         true,
		AckMessage:     ack, // Include ack message so AI knows what was already sent
		Metadata: map[string]string{
			"telnyx_event_id": evt.ID,
			"telnyx_call_id":  payload.ID,
			"telnyx_status":   payload.Status,
		},
	}
	jobID := fmt.Sprintf("telnyx:voice:%s", evt.ID)
	publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	opts := []conversation.PublishOption{conversation.WithoutJobTracking()}
	if h.trackJobs {
		opts = nil
	}
	if err := h.conversation.EnqueueStart(publishCtx, jobID, startReq, opts...); err != nil {
		return fmt.Errorf("enqueue missed-call start: %w", err)
	}
	h.appendTranscript(context.Background(), conversationID, conversation.SMSTranscriptMessage{
		Role: "assistant",
		From: to,
		To:   from,
		Body: ack,
		Kind: "voice_ack",
	})
	h.sendAutoReply(context.Background(), to, from, ack)
	return nil
}

// HandleMissedCall implements MissedCallTexter. Triggers the text-back flow
// for a missed call, used by CallControlHandler when voice AI is not enabled.
func (h *TelnyxWebhookHandler) HandleMissedCall(ctx context.Context, from, to string) error {
	h.logger.Info("missed-call-texter: triggered from call control", "from", from, "to", to)
	clinicID, err := h.store.LookupClinicByNumber(ctx, to)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.logger.Warn("missed-call-texter: clinic not found for number", "to", to)
			return fmt.Errorf("%w: %s", errClinicNotFound, to)
		}
		return fmt.Errorf("lookup clinic for %s: %w", to, err)
	}
	orgID := clinicID.String()
	leadID := fmt.Sprintf("%s:%s", orgID, from)
	if h.leads != nil {
		lead, err := h.leads.GetOrCreateByPhone(ctx, orgID, from, "telnyx_voice", "")
		if err != nil {
			return fmt.Errorf("persist lead: %w", err)
		}
		if lead != nil && lead.ID != "" {
			leadID = lead.ID
		}
	}
	conversationID := telnyxConversationID(orgID, from)
	ack := h.voiceAckMessage(ctx, orgID)
	startReq := conversation.StartRequest{
		OrgID:          orgID,
		LeadID:         leadID,
		ConversationID: conversationID,
		Intro:          "We just missed your call. I can help you book an appointment or answer quick questions by text.",
		Source:         "telnyx_voice",
		ClinicID:       orgID,
		Channel:        conversation.ChannelSMS,
		From:           from,
		To:             to,
		Silent:         true,
		AckMessage:     ack,
		Metadata: map[string]string{
			"call_control_reject": "true",
		},
	}
	jobID := fmt.Sprintf("cc:missed:%s:%s", from, to)
	publishCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	opts := []conversation.PublishOption{conversation.WithoutJobTracking()}
	if h.trackJobs {
		opts = nil
	}
	if err := h.conversation.EnqueueStart(publishCtx, jobID, startReq, opts...); err != nil {
		return fmt.Errorf("enqueue missed-call start: %w", err)
	}
	h.appendTranscript(context.Background(), conversationID, conversation.SMSTranscriptMessage{
		Role: "assistant",
		From: to,
		To:   from,
		Body: ack,
		Kind: "voice_ack",
	})
	h.sendAutoReply(context.Background(), to, from, ack)
	return nil
}

// isTelnyxMissedCall returns true when the voice event represents a call that
// was not answered by the callee.
func isTelnyxMissedCall(eventType, status, hangup string) bool {
	status = strings.ToLower(status)
	hangup = strings.ToLower(hangup)
	switch status {
	case "no-answer", "no_answer", "busy", "failed", "canceled", "cancelled", "not_answered", "timeout", "voicemail":
		return true
	}
	switch hangup {
	case "no-answer", "no_answer", "busy", "cancel", "canceled", "timeout", "declined",
		"originator_cancel", "originator-cancel": // caller hung up before answer
		return true
	}
	et := strings.ToLower(eventType)
	return strings.Contains(et, "hangup") || strings.Contains(et, "no_answer")
}
