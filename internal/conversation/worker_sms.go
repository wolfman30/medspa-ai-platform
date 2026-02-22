package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/booking"
)

func (w *Worker) sendReply(ctx context.Context, payload queuePayload, resp *Response) bool {
	if resp == nil || resp.Message == "" {
		return false
	}
	msg := payload.Message
	// Voice channel responses are returned synchronously via the webhook handler,
	// not sent as SMS. Log the response for transcript but skip SMS delivery.
	if msg.Channel == ChannelVoice {
		w.appendTranscript(ctx, resp.ConversationID, SMSTranscriptMessage{
			Role:      "assistant",
			From:      msg.To,
			To:        msg.From,
			Body:      resp.Message,
			Timestamp: resp.Timestamp,
			Kind:      "voice_reply",
		})
		return false
	}
	if msg.Channel == ChannelInstagram {
		return w.sendInstagramReply(ctx, payload, resp)
	}
	if msg.Channel == ChannelWebChat {
		return w.sendWebChatReply(ctx, payload, resp)
	}
	if msg.Channel != ChannelSMS {
		return false
	}
	if msg.From == "" || msg.To == "" {
		return false
	}
	if w.isOptedOut(ctx, msg.OrgID, msg.From) {
		return false
	}
	if w.msgChecker != nil {
		providerID := providerMessageID(msg.Metadata)
		if providerID != "" {
			exists, err := w.msgChecker.HasProviderMessage(ctx, providerID)
			if err != nil {
				w.logger.Warn("provider message lookup failed", "error", err, "provider_message_id", providerID, "job_id", payload.ID)
			} else if !exists {
				w.logger.Info("suppressing reply: inbound message missing", "provider_message_id", providerID, "job_id", payload.ID)
				return true
			}
		}
	}

	outboundText, blocked := w.applySupervisor(ctx, SupervisorRequest{
		OrgID:          msg.OrgID,
		ConversationID: msg.ConversationID,
		LeadID:         msg.LeadID,
		Channel:        msg.Channel,
		UserMessage:    msg.Message,
		DraftMessage:   resp.Message,
	})
	if blocked {
		resp = &Response{
			ConversationID: resp.ConversationID,
			Message:        outboundText,
			Timestamp:      time.Now().UTC(),
		}
	} else if outboundText != resp.Message {
		resp = &Response{
			ConversationID: resp.ConversationID,
			Message:        outboundText,
			Timestamp:      resp.Timestamp,
		}
	}

	// Output guard: scan reply for sensitive data leaks before sending.
	leakResult := ScanOutputForLeaks(resp.Message)
	if leakResult.Leaked {
		for _, reason := range leakResult.Reasons {
			w.events.OutputGuardTriggered(ctx, resp.ConversationID, msg.OrgID, reason)
		}
		w.logger.Warn("output guard: sensitive data leak detected",
			"conversation_id", resp.ConversationID,
			"org_id", msg.OrgID,
			"reasons", leakResult.Reasons,
		)
		if leakResult.Sanitized == "" {
			// Can't salvage — use generic fallback
			resp = &Response{
				ConversationID: resp.ConversationID,
				Message:        defaultSupervisorFallback,
				Timestamp:      time.Now().UTC(),
			}
		} else {
			resp = &Response{
				ConversationID: resp.ConversationID,
				Message:        leakResult.Sanitized,
				Timestamp:      resp.Timestamp,
			}
		}
	}

	conversationID := strings.TrimSpace(resp.ConversationID)
	if conversationID == "" {
		conversationID = strings.TrimSpace(msg.ConversationID)
	}

	reply := OutboundReply{
		OrgID:          msg.OrgID,
		LeadID:         msg.LeadID,
		ConversationID: conversationID,
		To:             msg.From,
		From:           msg.To,
		Body:           resp.Message,
		Metadata: map[string]string{
			"job_id": payload.ID,
		},
	}

	var sendErr error
	if w.messenger != nil {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := w.messenger.SendReply(sendCtx, reply); err != nil {
			sendErr = err
			w.logger.Error("failed to send outbound reply", "error", err, "job_id", payload.ID, "org_id", msg.OrgID)
		}
	}

	providerMessageID := ""
	providerStatus := ""
	if reply.Metadata != nil {
		providerMessageID = strings.TrimSpace(reply.Metadata["provider_message_id"])
		providerStatus = strings.TrimSpace(reply.Metadata["provider_status"])
	}
	if providerStatus == "" && w.messenger != nil {
		if sendErr != nil {
			providerStatus = "failed"
		} else {
			providerStatus = "sent"
		}
	}
	errorReason := ""
	if sendErr != nil {
		errorReason = sendErr.Error()
	}

	w.appendTranscript(context.Background(), conversationID, SMSTranscriptMessage{
		Role:              "assistant",
		From:              msg.To,
		To:                msg.From,
		Body:              resp.Message,
		Timestamp:         resp.Timestamp,
		Kind:              "ai_reply",
		ProviderMessageID: providerMessageID,
		Status:            providerStatus,
		ErrorReason:       errorReason,
	})
	return blocked
}

func (w *Worker) applySupervisor(ctx context.Context, req SupervisorRequest) (string, bool) {
	if w == nil || w.supervisor == nil {
		return req.DraftMessage, false
	}
	draft := strings.TrimSpace(req.DraftMessage)
	if draft == "" {
		return req.DraftMessage, false
	}
	mode := w.supervisorMode
	if mode == "" {
		mode = SupervisorModeWarn
	}
	decision, err := w.supervisor.Review(ctx, req)
	if err != nil {
		w.logger.Warn("supervisor review failed; allowing reply", "error", err, "mode", mode)
		return req.DraftMessage, false
	}
	action := decision.Action
	switch mode {
	case SupervisorModeWarn:
		if action != SupervisorActionAllow {
			w.logger.Warn("supervisor flagged reply", "action", action, "reason", decision.Reason)
		}
		return req.DraftMessage, false
	case SupervisorModeBlock:
		switch action {
		case SupervisorActionBlock:
			w.logger.Warn("supervisor blocked reply", "reason", decision.Reason)
			return defaultSupervisorFallback, true
		case SupervisorActionEdit:
			if strings.TrimSpace(decision.EditedText) != "" {
				w.logger.Info("supervisor edited reply", "reason", decision.Reason)
				return decision.EditedText, false
			}
			w.logger.Warn("supervisor edit missing content; allowing reply", "reason", decision.Reason)
			return req.DraftMessage, false
		default:
			return req.DraftMessage, false
		}
	case SupervisorModeEdit:
		switch action {
		case SupervisorActionEdit:
			if strings.TrimSpace(decision.EditedText) != "" {
				w.logger.Info("supervisor edited reply", "reason", decision.Reason)
				return decision.EditedText, false
			}
			w.logger.Warn("supervisor edit missing content; allowing reply", "reason", decision.Reason)
			return req.DraftMessage, false
		case SupervisorActionBlock:
			w.logger.Warn("supervisor blocked reply", "reason", decision.Reason)
			return defaultSupervisorFallback, true
		default:
			return req.DraftMessage, false
		}
	default:
		w.logger.Warn("supervisor mode unknown; allowing reply", "mode", mode)
		return req.DraftMessage, false
	}
}

func (w *Worker) handleTimeSelectionResponse(ctx context.Context, msg MessageRequest, resp *Response) {
	if resp == nil || resp.TimeSelectionResponse == nil {
		return
	}
	if w.isOptedOut(ctx, msg.OrgID, msg.From) {
		return
	}

	tsr := resp.TimeSelectionResponse

	// Send the time selection SMS
	if tsr.SMSMessage != "" && w.messenger != nil {
		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: msg.ConversationID,
			To:             msg.From, // Send to the customer
			From:           msg.To,   // From the clinic number
			Body:           tsr.SMSMessage,
		}
		if err := w.messenger.SendReply(ctx, reply); err != nil {
			w.logger.Error("failed to send time selection SMS", "error", err, "org_id", msg.OrgID, "lead_id", msg.LeadID)
			return
		}

		// Record to transcript + database
		timeSelMsg := SMSTranscriptMessage{
			Role:      "assistant",
			Body:      tsr.SMSMessage,
			From:      msg.To,
			To:        msg.From,
			Timestamp: time.Now(),
		}
		if w.transcript != nil {
			_ = w.transcript.Append(ctx, msg.ConversationID, timeSelMsg)
		}
		if w.convStore != nil {
			_ = w.convStore.AppendMessage(ctx, msg.ConversationID, timeSelMsg)
		}

		// Save time options to LLM conversation history if not already saved by the LLM service.
		// Without this, the LLM won't know what times were presented when the
		// patient replies with a slot number (e.g. "6"), causing confusion.
		if !tsr.SavedToHistory && w.processor != nil {
			if histStore, ok := w.processor.(interface {
				AppendAssistantMessage(ctx context.Context, conversationID, message string) error
			}); ok {
				if err := histStore.AppendAssistantMessage(ctx, msg.ConversationID, tsr.SMSMessage); err != nil {
					w.logger.Warn("failed to save time selection to LLM history", "error", err)
				}
			}
		}
	}

	// Update conversation status to awaiting_time_selection
	if w.convStore != nil {
		if err := w.convStore.UpdateStatus(ctx, msg.ConversationID, StatusAwaitingTimeSelection); err != nil {
			w.logger.Warn("failed to update conversation status to awaiting_time_selection", "error", err, "conversation_id", msg.ConversationID)
		}
	}

	w.logger.Info("time selection SMS sent",
		"conversation_id", msg.ConversationID,
		"slots_presented", len(tsr.Slots),
		"service", tsr.Service,
		"exact_match", tsr.ExactMatch,
	)
}

func (w *Worker) sendBookingFallbackSMS(ctx context.Context, msg MessageRequest, body string) {
	if w.messenger == nil {
		return
	}
	reply := OutboundReply{
		OrgID:          msg.OrgID,
		LeadID:         msg.LeadID,
		ConversationID: msg.ConversationID,
		To:             msg.From,
		From:           msg.To,
		Body:           body,
	}
	if err := w.messenger.SendReply(ctx, reply); err != nil {
		w.logger.Error("failed to send booking fallback SMS", "error", err, "org_id", msg.OrgID)
	}
	w.appendTranscript(ctx, msg.ConversationID, SMSTranscriptMessage{
		Role:      "assistant",
		From:      msg.To,
		To:        msg.From,
		Body:      body,
		Timestamp: time.Now(),
		Kind:      "booking_fallback",
	})
}

func (w *Worker) handleManualHandoff(ctx context.Context, msg MessageRequest, resp *Response) {
	if w.manualHandoff == nil || resp == nil {
		return
	}
	if w.isOptedOut(ctx, msg.OrgID, msg.From) {
		return
	}

	cfg := w.clinicConfig(ctx, msg.OrgID)
	clinicName := "the clinic"
	if cfg != nil {
		clinicName = cfg.Name
	}

	// Build lead summary from available data
	lead := booking.LeadSummary{
		OrgID:          msg.OrgID,
		LeadID:         msg.LeadID,
		ConversationID: msg.ConversationID,
		ClinicName:     clinicName,
		PatientPhone:   msg.From,
		CollectedAt:    time.Now().UTC(),
	}

	// Enrich from leads repository if available
	if w.leadsRepo != nil && msg.LeadID != "" {
		if dbLead, err := w.leadsRepo.GetByID(ctx, msg.OrgID, msg.LeadID); err == nil && dbLead != nil {
			lead.PatientName = dbLead.Name
			lead.PatientEmail = dbLead.Email
			lead.ServiceRequested = dbLead.ServiceInterest
			lead.PatientType = dbLead.PatientType
			lead.PreferredDays = dbLead.PreferredDays
			lead.PreferredTimes = dbLead.PreferredTimes
			lead.ConversationNotes = dbLead.SchedulingNotes
		}
	}

	result, err := w.manualHandoff.CreateBooking(ctx, lead)
	if err != nil {
		w.logger.Error("manual handoff notification failed (non-fatal)",
			"error", err,
			"org_id", msg.OrgID,
			"lead_id", msg.LeadID,
		)
		// Continue — still send the patient message even if clinic notification failed
	}

	// Send handoff confirmation to patient
	handoffMsg := result.HandoffMessage
	if handoffMsg == "" {
		handoffMsg = w.manualHandoff.GetHandoffMessage(clinicName)
	}
	if w.messenger != nil {
		reply := OutboundReply{
			OrgID:          msg.OrgID,
			LeadID:         msg.LeadID,
			ConversationID: msg.ConversationID,
			To:             msg.From,
			From:           msg.To,
			Body:           handoffMsg,
		}
		if err := w.messenger.SendReply(ctx, reply); err != nil {
			w.logger.Error("failed to send manual handoff SMS to patient",
				"error", err,
				"org_id", msg.OrgID,
				"lead_id", msg.LeadID,
			)
			return
		}

		// Record to transcript
		handoffTranscript := SMSTranscriptMessage{
			Role:      "assistant",
			Body:      handoffMsg,
			From:      msg.To,
			To:        msg.From,
			Timestamp: time.Now(),
		}
		w.appendTranscript(ctx, msg.ConversationID, handoffTranscript)
		if w.convStore != nil {
			_ = w.convStore.AppendMessage(ctx, msg.ConversationID, handoffTranscript)
		}
	}

	w.logger.Info("manual handoff completed",
		"org_id", msg.OrgID,
		"lead_id", msg.LeadID,
		"patient_name", lead.PatientName,
		"service", lead.ServiceRequested,
	)
}

func smsConversationID(orgID string, phone string) string {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return ""
	}
	digits := sanitizeDigits(phone)
	digits = normalizeUSDigits(digits)
	if digits == "" {
		return ""
	}
	return fmt.Sprintf("sms:%s:%s", orgID, digits)
}
